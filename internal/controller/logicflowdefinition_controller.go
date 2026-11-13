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
	"net/http"
	"net/url"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/open-workflow-specification/sdk-go/v4/model"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	sigsyaml "sigs.k8s.io/yaml"
)

const ConfigMapPrefix = "lfd-"

const definitionRegistrationRequeueInterval = 10 * time.Second

var definitionRuntimeHTTPClient = &http.Client{
	Timeout:   3 * time.Second,
	Transport: &http.Transport{Proxy: nil},
}

// LogicFlowDefinitionReconciler reconciles a LogicFlowDefinition object
type LogicFlowDefinitionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowservices,verbs=list;watch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/finalizers,verbs=update
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *LogicFlowDefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var def logicv1.LogicFlowDefinition
	if err := r.Get(ctx, req.NamespacedName, &def); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Parse flow document
	wf, err := def.Spec.ParseFlow()
	if err != nil {
		log.Error(err, "failed to parse flow document")
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionFlowParsed, metav1.ConditionFalse, def.Generation, logicv1.ReasonParseError, err.Error())
		return ctrl.Result{}, r.updateStatusWithConflictRetry(ctx, &def)
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionFlowParsed, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	if changed, err := r.updateFlowIDLabels(ctx, &def, wf); err != nil {
		return ctrl.Result{}, err
	} else if changed {
		return ctrl.Result{}, r.updateStatus(ctx, &def, wf)
	}

	conflict, err := r.checkRuntimeConsistency(ctx, &def, wf)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !conflict {
		if err := r.applyConfigMap(ctx, &def, wf); err != nil {
			log.Error(err, "failed to apply ConfigMap")
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionConfigMapReady, metav1.ConditionFalse, def.Generation, logicv1.ReasonSSAApplyFailed, err.Error())
		} else {
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionConfigMapReady, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")
		}
	}

	// Validate runtimeRef exists
	var rt logicv1.LogicFlowRuntime
	rtKey := client.ObjectKey{Name: def.Spec.RuntimeRef.Name, Namespace: def.Namespace}
	if err := r.Get(ctx, rtKey, &rt); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("referenced LogicFlowRuntime not found", "runtimeRef", def.Spec.RuntimeRef.Name)
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRefValid, metav1.ConditionFalse, def.Generation, logicv1.ReasonRuntimeNotFound, fmt.Sprintf("LogicFlowRuntime %q not found", def.Spec.RuntimeRef.Name))
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRegistered, metav1.ConditionFalse, def.Generation, logicv1.ReasonAwaitingRegistration, "Waiting for the referenced runtime")
			return ctrl.Result{RequeueAfter: time.Second * 30}, r.updateStatus(ctx, &def, wf)
		}
		return ctrl.Result{}, err
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRefValid, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	registered, reason, message := r.checkRuntimeRegistration(ctx, &rt, wf)
	if registered {
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRegistered, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady,
			fmt.Sprintf("Runtime %q reports workflow %s/%s version %s", rt.Name, def.Namespace, wf.Document.Name, wf.Document.Version))
	} else {
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRegistered, metav1.ConditionFalse, def.Generation, reason, message)
	}
	result := ctrl.Result{}
	if !registered {
		result.RequeueAfter = definitionRegistrationRequeueInterval
	}

	return result, r.updateStatus(ctx, &def, wf)
}

func (r *LogicFlowDefinitionReconciler) checkRuntimeRegistration(ctx context.Context, rt *logicv1.LogicFlowRuntime, wf *model.Workflow) (bool, string, string) {
	path := fmt.Sprintf("/q/flow/%s/%s/%s/instances",
		url.PathEscape(wf.Document.Namespace),
		url.PathEscape(wf.Document.Name),
		url.PathEscape(wf.Document.Version))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s.%s.svc:%d%s", rt.Name, rt.Namespace, defaultPort, path), nil)
	if err != nil {
		return false, logicv1.ReasonRuntimeProbeFailed, fmt.Sprintf("build runtime instance request: %v", err)
	}
	response, err := definitionRuntimeHTTPClient.Do(request)
	if err != nil {
		return false, logicv1.ReasonRuntimeProbeFailed, fmt.Sprintf("query runtime %q workflow instances: %v", rt.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return false, logicv1.ReasonAwaitingRegistration,
			fmt.Sprintf("runtime %q has not registered workflow %s/%s version %s (HTTP %d)", rt.Name, wf.Document.Namespace, wf.Document.Name, wf.Document.Version, response.StatusCode)
	}
	return true, logicv1.ReasonReady, ""
}

func configMapName(def *logicv1.LogicFlowDefinition) string {
	return ConfigMapPrefix + def.Name
}

func (r *LogicFlowDefinitionReconciler) applyConfigMap(ctx context.Context, def *logicv1.LogicFlowDefinition, wf *model.Workflow) error {
	dataKey := wf.Document.Name + ".yaml"

	childLabels := ChildLabels(def)
	childLabels[logicv1.LabelRuntimeRef] = def.Spec.RuntimeRef.Name
	childLabels[logicv1.LabelWorkflowName] = wf.Document.Name
	childLabels[logicv1.LabelWorkflowVersion] = wf.Document.Version
	childLabels[logicv1.LabelWorkflowNamespace] = def.Namespace

	wf.Document.Namespace = def.Namespace
	flowYAML, err := sigsyaml.Marshal(wf)
	if err != nil {
		return fmt.Errorf("failed to serialize flow document: %w", err)
	}

	cm := corev1ac.ConfigMap(configMapName(def), def.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(def, logicv1.LogicFlowDefinitionKind)).
		WithData(map[string]string{
			dataKey: string(flowYAML),
		})

	return r.Apply(ctx, cm, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowDefinitionReconciler) updateStatus(ctx context.Context, def *logicv1.LogicFlowDefinition, wf *model.Workflow) (err error) {
	def.Status.ObservedGeneration = def.Generation
	def.Status.WorkflowName = wf.Document.Name
	def.Status.WorkflowVersion = wf.Document.Version
	def.Status.WorkflowNamespace = def.Namespace
	def.Status.ConfigMapRef = &corev1.LocalObjectReference{Name: configMapName(def)}
	def.Status.FlowServiceRef, err = r.checkServiceRelationship(ctx, def)
	if err != nil {
		return err
	}

	return r.updateStatusWithConflictRetry(ctx, def)
}

func (r *LogicFlowDefinitionReconciler) updateStatusWithConflictRetry(ctx context.Context, def *logicv1.LogicFlowDefinition) error {
	setDefinitionReadyCondition(def)
	savedStatus := def.Status.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, client.ObjectKeyFromObject(def), def); err != nil {
			return err
		}
		if apiequality.Semantic.DeepEqual(def.Status, *savedStatus) {
			return nil
		}
		// Restore accumulated changes onto freshly fetched object
		def.Status = *savedStatus
		return r.Status().Update(ctx, def)
	})
}

func setDefinitionReadyCondition(def *logicv1.LogicFlowDefinition) {
	if definitionReady(def) {
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionReady, metav1.ConditionTrue,
			def.Generation, logicv1.ReasonReady, "Definition is registered in the referenced runtime")
		return
	}
	requiredConditions := []string{
		logicv1.ConditionFlowParsed,
		logicv1.ConditionRuntimeConsistent,
		logicv1.ConditionConfigMapReady,
		logicv1.ConditionRuntimeRefValid,
		logicv1.ConditionRuntimeRegistered,
	}
	for _, conditionType := range requiredConditions {
		condition := logicv1.FindCondition(def.Status.Conditions, conditionType)
		if condition == nil {
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionReady, metav1.ConditionFalse,
				def.Generation, "Reconciling", fmt.Sprintf("Waiting for %s condition", conditionType))
			return
		}
		if condition.Status != metav1.ConditionTrue {
			message := condition.Message
			if message == "" {
				message = fmt.Sprintf("%s condition is %s", conditionType, condition.Status)
			}
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionReady, metav1.ConditionFalse,
				def.Generation, condition.Reason, message)
			return
		}
	}
}

func definitionReady(def *logicv1.LogicFlowDefinition) bool {
	requiredConditions := []string{
		logicv1.ConditionFlowParsed,
		logicv1.ConditionRuntimeConsistent,
		logicv1.ConditionConfigMapReady,
		logicv1.ConditionRuntimeRefValid,
		logicv1.ConditionRuntimeRegistered,
	}
	for _, conditionType := range requiredConditions {
		condition := logicv1.FindCondition(def.Status.Conditions, conditionType)
		if condition == nil || condition.Status != metav1.ConditionTrue {
			return false
		}
	}
	return true
}

func (r *LogicFlowDefinitionReconciler) updateFlowIDLabels(ctx context.Context, def *logicv1.LogicFlowDefinition, wf *model.Workflow) (bool, error) {
	if def.Labels == nil {
		def.Labels = make(map[string]string)
	}
	if def.Labels[logicv1.LabelWorkflowName] == wf.Document.Name &&
		def.Labels[logicv1.LabelWorkflowVersion] == wf.Document.Version &&
		def.Labels[logicv1.LabelWorkflowNamespace] == def.Namespace &&
		def.Labels[logicv1.LabelRuntimeRef] == def.Spec.RuntimeRef.Name {
		return false, nil
	}
	patch := client.MergeFrom(def.DeepCopy())
	def.Labels[logicv1.LabelWorkflowName] = wf.Document.Name
	def.Labels[logicv1.LabelWorkflowVersion] = wf.Document.Version
	def.Labels[logicv1.LabelWorkflowNamespace] = def.Namespace
	def.Labels[logicv1.LabelRuntimeRef] = def.Spec.RuntimeRef.Name
	if err := r.Patch(ctx, def, patch); err != nil {
		return false, err
	}
	return true, nil
}

func (r *LogicFlowDefinitionReconciler) checkRuntimeConsistency(ctx context.Context, def *logicv1.LogicFlowDefinition, wf *model.Workflow) (bool, error) {
	var siblings logicv1.LogicFlowDefinitionList
	if err := r.List(ctx, &siblings,
		client.InNamespace(def.Namespace),
		client.MatchingLabels{
			logicv1.LabelWorkflowName:      wf.Document.Name,
			logicv1.LabelWorkflowNamespace: def.Namespace,
		},
	); err != nil {
		return false, err
	}
	for i := range siblings.Items {
		if siblings.Items[i].Name == def.Name {
			continue
		}
		if siblings.Items[i].Spec.RuntimeRef.Name != def.Spec.RuntimeRef.Name {
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeConsistent, metav1.ConditionFalse, def.Generation, logicv1.ReasonRuntimeConflict,
				fmt.Sprintf("workflow %s/%s has conflicting runtimes: %q vs %q",
					def.Namespace, wf.Document.Name,
					def.Spec.RuntimeRef.Name, siblings.Items[i].Spec.RuntimeRef.Name))
			return true, nil
		}
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeConsistent, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")
	return false, nil
}

func (r *LogicFlowDefinitionReconciler) mapRuntimeToDefinitions(ctx context.Context, obj client.Object) []reconcile.Request {
	var defs logicv1.LogicFlowDefinitionList
	if err := r.List(ctx, &defs,
		client.InNamespace(obj.GetNamespace()),
		client.MatchingLabels{logicv1.LabelRuntimeRef: obj.GetName()},
	); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, len(defs.Items))
	for i := range defs.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{Name: defs.Items[i].Name, Namespace: defs.Items[i].Namespace},
		}
	}
	return requests
}

func (r *LogicFlowDefinitionReconciler) mapServiceToDefinitions(ctx context.Context, obj client.Object) []reconcile.Request {
	svc, ok := obj.(*logicv1.LogicFlowService)
	if !ok {
		return nil
	}
	refs := make(map[string]struct{})
	if svc.Spec.DefaultDefinition != nil {
		refs[svc.Spec.DefaultDefinition.Name] = struct{}{}
	}
	for _, traffic := range svc.Spec.Traffic {
		refs[traffic.DefinitionRef.Name] = struct{}{}
	}

	var defs logicv1.LogicFlowDefinitionList
	if err := r.List(ctx, &defs, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(refs))
	for i := range defs.Items {
		def := &defs.Items[i]
		_, referenced := refs[def.Name]
		previouslyLinked := def.Status.FlowServiceRef != nil && def.Status.FlowServiceRef.Name == svc.Name
		if !referenced && !previouslyLinked {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
			Name: defs.Items[i].Name, Namespace: defs.Items[i].Namespace,
		}})
	}
	return requests
}

func (r *LogicFlowDefinitionReconciler) checkServiceRelationship(ctx context.Context, def *logicv1.LogicFlowDefinition) (*corev1.LocalObjectReference, error) {
	var services logicv1.LogicFlowServiceList
	if err := r.List(ctx, &services, client.InNamespace(def.Namespace)); err != nil {
		return nil, fmt.Errorf("list LogicFlowServices in namespace %q: %w", def.Namespace, err)
	}

	for i := range services.Items {
		svc := &services.Items[i]
		if svc.Spec.DefaultDefinition != nil && svc.Spec.DefaultDefinition.Name == def.Name {
			return &corev1.LocalObjectReference{Name: svc.Name}, nil
		}
		for _, traffic := range svc.Spec.Traffic {
			if traffic.DefinitionRef.Name == def.Name {
				return &corev1.LocalObjectReference{Name: svc.Name}, nil
			}
		}
	}
	return nil, nil
}

func (r *LogicFlowDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowDefinition{}).
		Owns(&corev1.ConfigMap{}).
		Watches(&logicv1.LogicFlowRuntime{},
			handler.EnqueueRequestsFromMapFunc(r.mapRuntimeToDefinitions),
		).
		Watches(&logicv1.LogicFlowService{},
			handler.EnqueueRequestsFromMapFunc(r.mapServiceToDefinitions),
		).
		Named("logicflowdefinition").
		Complete(r)
}
