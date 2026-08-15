/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/kubesmarts/logic-operator/utils"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

// LogicFlowServiceReconciler reconciles a LogicFlowService object
type LogicFlowServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

func (r *LogicFlowServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var svc logicv1.LogicFlowService
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defs, err := r.resolveDefinitions(ctx, &svc)
	if err != nil {
		log.Error(err, "failed to resolve definitions")
		return ctrl.Result{}, err
	}

	rt, err := r.resolveRuntime(ctx, &svc, defs)
	if err != nil {
		log.Error(err, "failed to resolve runtime")
		return ctrl.Result{}, err
	}

	wfName := defs[0].Labels[logicv1.LabelWorkflowName]
	wfNamespace := defs[0].Labels[logicv1.LabelWorkflowNamespace]

	if wfName == "" || wfNamespace == "" {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Set conditions to true, let the reconciliation turn to false if needed, update status by the end
	logicv1.SetConditionTrue(&svc.Status.Conditions, logicv1.ConditionIngressReady, svc.Generation, logicv1.ReasonReady)

	if err := r.applyNetworking(ctx, &svc, rt, defs, wfNamespace, wfName); err != nil {
		log.Error(err, "failed to apply networking")
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, &svc, rt, wfNamespace, wfName); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicFlowServiceReconciler) applyNetworking(
	ctx context.Context,
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	defs []logicv1.LogicFlowDefinition,
	wfNamespace, wfName string,
) error {
	svc.Status.IngressRef = nil
	svc.Status.RouteRef = nil
	svc.Status.HTTPRouteRef = nil

	if svc.Spec.Ingress.GatewayRef == nil && !utils.IsOpenShift() && svc.Spec.Ingress.Host == "" {
		logicv1.SetCondition(&svc.Status.Conditions, logicv1.ConditionIngressReady,
			metav1.ConditionFalse, svc.Generation,
			logicv1.ReasonIngressMisconfigured,
			"spec.ingress.host is required for nginx Ingress mode")
		return nil
	}

	if svc.Spec.Ingress.GatewayRef != nil {
		return r.applyHTTPRoute(ctx, svc, rt, defs, wfNamespace, wfName)
	}

	if svc.Spec.DefaultDefinition != nil {
		if err := r.deleteStaleCanaryIngresses(ctx, svc); err != nil {
			return err
		}
		version := defs[0].Labels[logicv1.LabelWorkflowVersion]
		if utils.IsOpenShift() {
			return r.applyRoute(ctx, svc, rt, wfNamespace, wfName, version)
		}
		return r.applyDefaultIngress(ctx, svc, rt, wfNamespace, wfName, version)
	}

	// Traffic[] without GatewayRef
	if utils.IsOpenShift() {
		logicv1.SetCondition(&svc.Status.Conditions, logicv1.ConditionIngressReady,
			metav1.ConditionFalse, svc.Generation,
			logicv1.ReasonGatewayRefRequired,
			"Traffic splitting on OpenShift requires gatewayRef to be set")
		return nil
	}

	if len(svc.Spec.Traffic) > 2 {
		logicv1.SetCondition(&svc.Status.Conditions, logicv1.ConditionIngressReady,
			metav1.ConditionFalse, svc.Generation,
			logicv1.ReasonGatewayRefRequired,
			"Nginx canary supports at most 2 traffic entries; use gatewayRef for 3+ versions")
		return nil
	}

	return r.applyCanaryIngresses(ctx, svc, rt, defs, wfNamespace, wfName)
}

func (r *LogicFlowServiceReconciler) applyDefaultIngress(
	ctx context.Context,
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName, version string,
) error {
	ingress := ingressForService(svc, rt, wfNamespace, wfName, version)
	svc.Status.IngressRef = &corev1.LocalObjectReference{Name: *ingress.Name}
	return r.Apply(ctx, ingress, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowServiceReconciler) deleteStaleCanaryIngresses(ctx context.Context, svc *logicv1.LogicFlowService) error {
	for _, suffix := range []string{"-canary", "-direct"} {
		ingress := &networkingv1.Ingress{}
		ingress.Name = svc.Name + suffix
		ingress.Namespace = svc.Namespace
		if err := r.Delete(ctx, ingress); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *LogicFlowServiceReconciler) applyCanaryIngresses(
	ctx context.Context,
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	defs []logicv1.LogicFlowDefinition,
	wfNamespace, wfName string,
) error {
	primary, canary := splitTrafficTargets(svc, defs)

	primaryIngress := ingressForService(svc, rt, wfNamespace, wfName, primary.version)
	if err := r.Apply(ctx, primaryIngress, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying primary ingress: %w", err)
	}

	canaryIng := canaryIngressForService(svc, rt, wfNamespace, wfName, canary.version, canary.weight)
	if err := r.Apply(ctx, canaryIng, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying canary ingress: %w", err)
	}

	directIng := directVersionIngress(svc, rt, wfNamespace, wfName)
	if err := r.Apply(ctx, directIng, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying direct version ingress: %w", err)
	}

	svc.Status.IngressRef = &corev1.LocalObjectReference{Name: *primaryIngress.Name}
	return nil
}

func (r *LogicFlowServiceReconciler) applyRoute(
	ctx context.Context,
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	wfNamespace, wfName, version string,
) error {
	desired := routeForDefault(svc, rt, wfNamespace, wfName, version)
	svc.Status.RouteRef = &corev1.LocalObjectReference{Name: desired.Name}
	//nolint:staticcheck // OpenShift Route API has no apply configurations; deprecated Patch+client.Apply is the only option
	return r.Patch(ctx, desired, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowServiceReconciler) applyHTTPRoute(
	ctx context.Context,
	svc *logicv1.LogicFlowService,
	rt *logicv1.LogicFlowRuntime,
	defs []logicv1.LogicFlowDefinition,
	wfNamespace, wfName string,
) error {
	if !utils.IsGatewayAPIAvailable() {
		logicv1.SetCondition(&svc.Status.Conditions, logicv1.ConditionIngressReady,
			metav1.ConditionFalse, svc.Generation,
			"GatewayAPINotAvailable",
			"Gateway API CRDs are not installed in the cluster")
		return nil
	}

	targets := buildTrafficTargets(svc, defs)
	desired := httpRouteForService(svc, rt, wfNamespace, wfName, targets)
	svc.Status.HTTPRouteRef = &corev1.LocalObjectReference{Name: *desired.Name}
	return r.Apply(ctx, desired, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func splitTrafficTargets(svc *logicv1.LogicFlowService, defs []logicv1.LogicFlowDefinition) (primary, canary trafficTarget) {
	primaryIdx := 0
	for i, t := range svc.Spec.Traffic {
		if t.Weight > svc.Spec.Traffic[primaryIdx].Weight {
			primaryIdx = i
		}
	}

	primary = trafficTarget{
		version: defs[primaryIdx].Labels[logicv1.LabelWorkflowVersion],
		weight:  svc.Spec.Traffic[primaryIdx].Weight,
	}
	canaryIdx := 1 - primaryIdx
	if canaryIdx < len(svc.Spec.Traffic) {
		canary = trafficTarget{
			version: defs[canaryIdx].Labels[logicv1.LabelWorkflowVersion],
			weight:  svc.Spec.Traffic[canaryIdx].Weight,
		}
	}
	return
}

func buildTrafficTargets(svc *logicv1.LogicFlowService, defs []logicv1.LogicFlowDefinition) []trafficTarget {
	if svc.Spec.DefaultDefinition != nil {
		return []trafficTarget{{
			version: defs[0].Labels[logicv1.LabelWorkflowVersion],
			weight:  100,
		}}
	}
	targets := make([]trafficTarget, len(svc.Spec.Traffic))
	for i, t := range svc.Spec.Traffic {
		targets[i] = trafficTarget{
			version: defs[i].Labels[logicv1.LabelWorkflowVersion],
			weight:  t.Weight,
		}
	}
	return targets
}

func (r *LogicFlowServiceReconciler) resolveDefinitions(ctx context.Context, svc *logicv1.LogicFlowService) ([]logicv1.LogicFlowDefinition, error) {
	if svc.Spec.DefaultDefinition != nil && len(svc.Spec.DefaultDefinition.Name) > 0 {
		def, err := r.resolveDefinition(ctx, svc.Spec.DefaultDefinition.Name, svc.Namespace)
		if err != nil {
			return nil, err
		}
		return []logicv1.LogicFlowDefinition{*def}, nil
	}

	wfName := ""
	wfRuntime := ""
	defList := make([]logicv1.LogicFlowDefinition, len(svc.Spec.Traffic))
	for i := range svc.Spec.Traffic {
		def, err := r.resolveDefinition(ctx, svc.Spec.Traffic[i].DefinitionRef.Name, svc.Namespace)
		if err != nil {
			return nil, err
		}
		if len(wfName) > 0 && wfName != def.Labels[logicv1.LabelWorkflowName] {
			return nil, fmt.Errorf("definition %s workflow name differs from %s. Traffic split must be amongst same workflows", def.Labels[logicv1.LabelWorkflowName], wfName)
		}
		if len(wfRuntime) > 0 && wfRuntime != def.Spec.RuntimeRef.Name {
			return nil, fmt.Errorf("definition %s runtime reference differs from %s. Traffic split must target the same runtime", def.Spec.RuntimeRef, wfRuntime)
		}
		wfName = def.Labels[logicv1.LabelWorkflowName]
		wfRuntime = def.Spec.RuntimeRef.Name
		defList[i] = *def
	}

	return defList, nil
}

func (r *LogicFlowServiceReconciler) resolveDefinition(ctx context.Context, name, namespace string) (*logicv1.LogicFlowDefinition, error) {
	var def logicv1.LogicFlowDefinition
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

func (r *LogicFlowServiceReconciler) resolveRuntime(ctx context.Context, svc *logicv1.LogicFlowService, defs []logicv1.LogicFlowDefinition) (*logicv1.LogicFlowRuntime, error) {
	if len(defs) == 0 {
		return nil, nil
	}

	var rt logicv1.LogicFlowRuntime
	if err := r.Get(ctx, types.NamespacedName{Name: defs[0].Spec.RuntimeRef.Name, Namespace: svc.Namespace}, &rt); err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *LogicFlowServiceReconciler) updateStatus(ctx context.Context, svc *logicv1.LogicFlowService, rt *logicv1.LogicFlowRuntime, _, _ string) error {
	svc.Status.ObservedGeneration = svc.Generation
	svc.Status.RuntimeRef = &corev1.LocalObjectReference{Name: rt.Name}
	svc.Status.Traffic = make([]logicv1.TrafficStatus, 0)
	if svc.Spec.DefaultDefinition != nil && len(svc.Spec.DefaultDefinition.Name) > 0 {
		svc.Status.Traffic = append(svc.Status.Traffic, logicv1.TrafficStatus{
			DefinitionRef: *svc.Spec.DefaultDefinition,
			Weight:        100,
			Ready:         rt.Status.ReadyReplicas > 0,
		})
	} else {
		for i := range svc.Spec.Traffic {
			svc.Status.Traffic = append(svc.Status.Traffic, logicv1.TrafficStatus{
				DefinitionRef: corev1.LocalObjectReference{Name: svc.Spec.Traffic[i].DefinitionRef.Name},
				Weight:        svc.Spec.Traffic[i].Weight,
				Ready:         rt.Status.ReadyReplicas > 0,
			})
		}
	}

	url, err := r.resolveURL(ctx, svc)
	if err != nil {
		return err
	}
	svc.Status.URL = url

	return r.Status().Update(ctx, svc)
}

func (r *LogicFlowServiceReconciler) resolveURL(ctx context.Context, svc *logicv1.LogicFlowService) (string, error) {
	scheme := "http"
	if svc.Spec.Ingress.TLS.Enabled {
		scheme = "https"
	}

	host, err := r.resolveHost(ctx, svc)
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", nil
	}

	return fmt.Sprintf("%s://%s/", scheme, host), nil
}

func (r *LogicFlowServiceReconciler) resolveHost(ctx context.Context, svc *logicv1.LogicFlowService) (string, error) {
	if svc.Spec.Ingress.GatewayRef != nil {
		var httpRoute gatewayv1.HTTPRoute
		if err := r.Get(ctx, client.ObjectKeyFromObject(svc), &httpRoute); err != nil {
			if apierrors.IsNotFound(err) {
				return "", nil
			}
			return "", err
		}
		if len(httpRoute.Spec.Hostnames) > 0 {
			return string(httpRoute.Spec.Hostnames[0]), nil
		}
		return "", nil
	}

	if utils.IsOpenShift() {
		var route routev1.Route
		if err := r.Get(ctx, client.ObjectKeyFromObject(svc), &route); err != nil {
			if apierrors.IsNotFound(err) {
				return "", nil
			}
			return "", err
		}
		for _, ingress := range route.Status.Ingress {
			if ingress.Host != "" {
				return ingress.Host, nil
			}
		}
		return route.Spec.Host, nil
	}

	var ingress networkingv1.Ingress
	if err := r.Get(ctx, client.ObjectKeyFromObject(svc), &ingress); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if len(ingress.Spec.Rules) > 0 && ingress.Spec.Rules[0].Host != "" {
		return ingress.Spec.Rules[0].Host, nil
	}
	for _, lb := range ingress.Status.LoadBalancer.Ingress {
		if lb.Hostname != "" {
			return lb.Hostname, nil
		}
		if lb.IP != "" {
			return lb.IP, nil
		}
	}
	return "", nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogicFlowServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowService{}).
		Owns(&networkingv1.Ingress{})

	if utils.IsOpenShift() {
		builder.Owns(&routev1.Route{})
	}

	if utils.IsGatewayAPIAvailable() {
		builder.Owns(&gatewayv1.HTTPRoute{})
	}

	return builder.Named("logicflowservice").Complete(r)
}
