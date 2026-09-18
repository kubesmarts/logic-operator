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

	"github.com/kubesmarts/logic-operator/utils"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LogicPlatformReconciler reconciles a LogicPlatform object
type LogicPlatformReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the LogicPlatform object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *LogicPlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	var rt logicv1.LogicPlatform
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.applyDeployment(ctx, &rt); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.applyService(ctx, &rt); err != nil {
		return ctrl.Result{}, err
	}

	// Apply Ingress/Route if enabled
	if rt.Spec.DataIndex.Ingress != nil && rt.Spec.DataIndex.Ingress.Enabled {
		if err := r.applyIngress(ctx, &rt); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.updateStatus(ctx, &rt); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicPlatformReconciler) applyDeployment(ctx context.Context, plat *logicv1.LogicPlatform) error {
	childLabels := ChildLabels(plat)
	opts := []ContainerOption{
		WithPersistenceEnvVars(plat.Spec.DataIndex.Persistence, plat.Namespace),
		WithFlywayPersistenceVars(),
		WithGraphQLVars(),
		DefaultProbes(),
	}
	spec := ToDeploymentSpec(
		ContainerNameDataIndex,
		&plat.Spec.DataIndex.Application,
		childLabels,
		SelectorLabels(plat.Name),
		opts...,
	)
	if plat.Spec.DataIndex.Persistence != nil {
		spec.Template.Spec.WithServiceAccountName(plat.Name)
	}
	deployment := appsv1ac.Deployment(plat.Name, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	svc := QuarkusService(plat, logicv1.LogicPlatformKind)
	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	if utils.IsOpenShift() {
		return r.applyRoute(ctx, plat)
	}
	return r.applyKubernetesIngress(ctx, plat)
}

func (r *LogicPlatformReconciler) applyKubernetesIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	desired := ingressForDataIndex(plat)
	plat.Status.IngressRef = &corev1.LocalObjectReference{Name: *desired.Name}
	plat.Status.RouteRef = nil
	return r.Apply(ctx, desired, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyRoute(ctx context.Context, plat *logicv1.LogicPlatform) error {
	desired := routeForDataIndex(plat)
	plat.Status.RouteRef = &corev1.LocalObjectReference{Name: desired.Name}
	plat.Status.IngressRef = nil
	//nolint:staticcheck // OpenShift Route API has no apply configurations; deprecated Patch+client.Apply is the only option
	return r.Patch(ctx, desired, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) updateStatusDeployment(ctx context.Context, plat *logicv1.LogicPlatform) error {
	var deployment appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(plat), &deployment)
	if apierrors.IsNotFound(err) {
		plat.Status.DataIndex.Service.Replicas.Desired = 0
		plat.Status.DataIndex.Service.Replicas.Current = 0
		plat.Status.DataIndex.Service.Replicas.Ready = 0
		plat.Status.DataIndex.Service.Replicas.Updated = 0
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexDeploymentAvailable,
			metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonDataIndexDeploymentNotFound, "")
		return nil
	}
	if err != nil {
		return err
	}

	// Update replica counts
	desiredReplicas := int32(1)
	if plat.Spec.DataIndex.Application.Replicas != nil {
		desiredReplicas = *plat.Spec.DataIndex.Application.Replicas
	}
	plat.Status.DataIndex.Service.Replicas.Desired = desiredReplicas
	plat.Status.DataIndex.Service.Replicas.Current = deployment.Status.Replicas
	plat.Status.DataIndex.Service.Replicas.Ready = deployment.Status.ReadyReplicas
	plat.Status.DataIndex.Service.Replicas.Updated = deployment.Status.UpdatedReplicas

	// Set deployment available condition based on deployment status
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexDeploymentAvailable,
		metav1.ConditionFalse, plat.Generation,
		logicv1.ReasonDeploymentProgressing, "")

	for _, cond := range deployment.Status.Conditions {
		switch cond.Type {
		case appsv1.DeploymentAvailable:
			switch cond.Status {
			case corev1.ConditionTrue:
				logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexDeploymentAvailable,
					metav1.ConditionTrue, plat.Generation, cond.Reason, cond.Message)
			case corev1.ConditionFalse:
				logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexDeploymentAvailable,
					metav1.ConditionFalse, plat.Generation, cond.Reason, cond.Message)
			}
		case appsv1.DeploymentProgressing:
			if cond.Status == corev1.ConditionFalse && cond.Reason == logicv1.ReasonProgressDeadlineExceeded {
				logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexDeploymentAvailable,
					metav1.ConditionFalse, plat.Generation, cond.Reason, cond.Message)
			}
		}
	}

	return nil
}

func (r *LogicPlatformReconciler) updateStatusService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	var svc corev1.Service
	err := r.Get(ctx, client.ObjectKeyFromObject(plat), &svc)
	if apierrors.IsNotFound(err) {
		plat.Status.DataIndex.Service.GraphQLEndpoint = ""
		plat.Status.DataIndex.Service.MetricsEndpoint = ""
		plat.Status.DataIndex.Service.URL = ""
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexServiceReady,
			metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonDataIndexServiceNotFound, "")
		return nil
	}
	if err != nil {
		return err
	}

	// Service exists, mark as ready
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexServiceReady,
		metav1.ConditionTrue, plat.Generation,
		logicv1.ReasonReady, "")

	// Determine base URL: use external URL if ingress enabled, otherwise internal service URL
	var baseURL string
	if plat.Spec.DataIndex.Ingress != nil && plat.Spec.DataIndex.Ingress.Enabled {
		externalURL, err := r.resolveExternalURL(ctx, plat)
		if err != nil {
			return err
		}
		if externalURL != "" {
			baseURL = externalURL
			plat.Status.DataIndex.Service.URL = externalURL
		} else {
			// Ingress enabled but URL not yet resolved, use internal
			baseURL = r.internalServiceURL(plat)
			plat.Status.DataIndex.Service.URL = ""
		}
	} else {
		// No ingress, use internal service URL
		baseURL = r.internalServiceURL(plat)
		plat.Status.DataIndex.Service.URL = ""
	}

	// Set endpoint URLs
	plat.Status.DataIndex.Service.GraphQLEndpoint = fmt.Sprintf("%s/graphql", baseURL)
	plat.Status.DataIndex.Service.MetricsEndpoint = fmt.Sprintf("%s/q/metrics", baseURL)

	return nil
}

func (r *LogicPlatformReconciler) internalServiceURL(plat *logicv1.LogicPlatform) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		plat.Name, plat.Namespace, QuarkusPort)
}

func (r *LogicPlatformReconciler) resolveExternalURL(ctx context.Context, plat *logicv1.LogicPlatform) (string, error) {
	return ResolveIngressURL(ctx, r.Client, client.ObjectKeyFromObject(plat), plat.Spec.DataIndex.Ingress.TLS.Enabled)
}

func (r *LogicPlatformReconciler) updateStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// Set observed generation
	plat.Status.ObservedGeneration = plat.Generation

	// Set resource references
	plat.Status.DataIndex.Service.DeploymentRef.Name = plat.Name
	plat.Status.DataIndex.Service.ServiceRef.Name = plat.Name

	// Update persistence config validation
	if err := r.updateStatusPersistence(ctx, plat); err != nil {
		return err
	}

	// Update deployment status
	if err := r.updateStatusDeployment(ctx, plat); err != nil {
		return err
	}

	// Update service status
	if err := r.updateStatusService(ctx, plat); err != nil {
		return err
	}

	// Set overall ready status based on replica count
	plat.Status.DataIndex.Service.Ready = plat.Status.DataIndex.Service.Replicas.Ready > 0

	// Derive phase from conditions
	phase := logicv1.DerivePhase(plat.Status.Conditions, plat.Status.DataIndex.Service.Replicas.Ready)
	plat.Status.Phase = logicv1.LogicPlatformStatusPhase(phase)

	// Update status in API server
	return r.Status().Update(ctx, plat)
}

func (r *LogicPlatformReconciler) updateStatusPersistence(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// If no persistence configured, clear status and mark as ready
	if plat.Spec.DataIndex.Persistence == nil || plat.Spec.DataIndex.Persistence.PostgreSQL == nil {
		plat.Status.DataIndex.Persistence = nil
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
			metav1.ConditionTrue, plat.Generation,
			logicv1.ReasonReady, "No persistence configured")
		return nil
	}

	pg := plat.Spec.DataIndex.Persistence.PostgreSQL
	status := &logicv1.PersistenceConfigStatus{
		Valid:        true,
		SecretExists: false,
	}

	// Check if secret exists
	var secret corev1.Secret
	secretKey := client.ObjectKey{
		Name:      pg.SecretRef.Name,
		Namespace: plat.Namespace,
	}
	err := r.Get(ctx, secretKey, &secret)
	if apierrors.IsNotFound(err) {
		status.Valid = false
		status.SecretExists = false
		status.Error = fmt.Sprintf("secret %s not found", pg.SecretRef.Name)
	} else if err != nil {
		return err
	} else {
		status.SecretExists = true
	}

	// Check if service exists (if using serviceRef)
	if pg.ServiceRef != nil {
		var svc corev1.Service
		svcKey := client.ObjectKey{
			Name:      pg.ServiceRef.Name,
			Namespace: plat.Namespace,
		}
		err := r.Get(ctx, svcKey, &svc)
		if apierrors.IsNotFound(err) {
			status.Valid = false
			status.ServiceExists = false
			if status.Error != "" {
				status.Error += "; "
			}
			status.Error += fmt.Sprintf("service %s not found", pg.ServiceRef.Name)
		} else if err != nil {
			return err
		} else {
			status.ServiceExists = true
		}
	}

	plat.Status.DataIndex.Persistence = status

	// Set condition based on validation
	if status.Valid {
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
			metav1.ConditionTrue, plat.Generation,
			logicv1.ReasonReady, "Persistence configuration is valid")
	} else {
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
			metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonPersistenceConfigInvalid, status.Error)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogicPlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicPlatform{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("logicplatform")

	// Add ownership for platform-specific resources
	if utils.IsOpenShift() {
		builder.Owns(&routev1.Route{})
	} else {
		builder.Owns(&networkingv1.Ingress{})
	}

	return builder.Complete(r)
}
