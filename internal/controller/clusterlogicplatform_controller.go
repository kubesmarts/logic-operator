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
	"errors"
	"fmt"
	"strings"

	"github.com/kubesmarts/logic-operator/utils"
	rbacv1 "k8s.io/api/rbac/v1"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// ClusterLogicPlatformReconciler reconciles a ClusterLogicPlatform object
type ClusterLogicPlatformReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	DatabaseConnector  DatabaseConnector
	platformComponents componentFactory // nil => defaultComponents
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=clusterlogicplatforms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=clusterlogicplatforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=clusterlogicplatforms/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterLogicPlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	var plat logicv1.ClusterLogicPlatform
	if err := r.Get(ctx, req.NamespacedName, &plat); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if target namespace exists
	var targetNs corev1.Namespace
	nsKey := client.ObjectKey{Name: plat.Spec.TargetNamespace}
	if err := r.Get(ctx, nsKey, &targetNs); err != nil {
		if apierrors.IsNotFound(err) {
			logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionTargetNamespaceResolved,
				metav1.ConditionFalse, plat.Generation,
				logicv1.ReasonTargetNamespaceMissing, fmt.Sprintf("Target namespace %s does not exist", plat.Spec.TargetNamespace))
			logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionReady,
				metav1.ConditionFalse, plat.Generation,
				logicv1.ReasonTargetNamespaceMissing, fmt.Sprintf("Target namespace %s does not exist", plat.Spec.TargetNamespace))
			if err := r.writeStatus(ctx, &plat); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Mark targetNamespace as resolved
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionTargetNamespaceResolved,
		metav1.ConditionTrue, plat.Generation,
		logicv1.ReasonReady, fmt.Sprintf("Target namespace %s exists", plat.Spec.TargetNamespace))

	comps := r.components()
	var errs []error

	// Phase 1: run all component Reconcile() independently
	for _, c := range comps {
		if err := c.Reconcile(ctx, &plat); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
		}
	}

	// Phase 2: run all component UpdateStatus() independently
	for _, c := range comps {
		if err := c.UpdateStatus(ctx, &plat); err != nil {
			errs = append(errs, fmt.Errorf("%s status: %w", c.Name(), err))
		}
	}

	// Phase 3: write the accumulated status once
	if err := r.writeStatus(ctx, &plat); err != nil {
		errs = append(errs, err)
	}

	return ctrl.Result{}, errors.Join(errs...)
}

func (r *ClusterLogicPlatformReconciler) writeStatus(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	// Set observed generation
	plat.Status.ObservedGeneration = plat.Generation

	// Update platform-level readiness
	updateClusterLogicPlatformReadiness(plat)

	// Write status in API server with retry on conflict
	savedStatus := plat.Status.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, client.ObjectKeyFromObject(plat), plat); err != nil {
			return err
		}
		// Restore accumulated changes onto freshly fetched object
		plat.Status = *savedStatus
		return r.Status().Update(ctx, plat)
	})
}

func updateClusterLogicPlatformReadiness(plat *logicv1.ClusterLogicPlatform) {
	var notReady []string
	if condition := logicv1.FindCondition(plat.Status.Conditions, logicv1.ConditionTargetNamespaceResolved); condition != nil && condition.Status != metav1.ConditionTrue {
		notReady = append(notReady, "Target Namespace")
	}
	components := 0
	if plat.Spec.DataIndex != nil {
		components++
		if plat.Status.DataIndex == nil || !plat.Status.DataIndex.Ready {
			notReady = append(notReady, "Data Index")
		}
	}
	if plat.Spec.Vector != nil {
		components++
		if plat.Status.Vector == nil || !plat.Status.Vector.Ready {
			notReady = append(notReady, "Vector")
		}
	}
	if plat.Spec.Gateway != nil {
		components++
		if plat.Status.Gateway == nil || !plat.Status.Gateway.Ready {
			notReady = append(notReady, "Workflow Gateway")
		}
	}

	if len(notReady) == 0 {
		message := "All enabled platform components are ready"
		if components == 0 {
			message = "No platform components are enabled"
		}
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionReady,
			metav1.ConditionTrue, plat.Generation, logicv1.ReasonReady, message)
		// A platform is one logical unit; the aggregate Ready condition supplies its availability.
		plat.Status.Phase = logicv1.DerivePhase(plat.Status.Conditions, 1)
		return
	}

	reason := logicv1.ReasonComponentsNotReady
	for _, conditionType := range []string{
		logicv1.ConditionDataIndexDeploymentAvailable,
		logicv1.ConditionDataIndexServiceReady,
		logicv1.ConditionDataIndexPersistenceReady,
		logicv1.ConditionVectorReady,
		logicv1.ConditionWorkflowGatewayReady,
	} {
		condition := logicv1.FindCondition(plat.Status.Conditions, conditionType)
		if condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == logicv1.ReasonProgressDeadlineExceeded {
			reason = logicv1.ReasonProgressDeadlineExceeded
			break
		}
	}
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionReady,
		metav1.ConditionFalse, plat.Generation, reason,
		fmt.Sprintf("Waiting for enabled platform components: %s", strings.Join(notReady, ", ")))
	// Zero available units keeps the aggregate phase Pending unless a condition marks it Failed.
	plat.Status.Phase = logicv1.DerivePhase(plat.Status.Conditions, 0)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterLogicPlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.ClusterLogicPlatform{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.ConfigMap{}).
		Named("clusterlogicplatform")

	// Add ownership for platform-specific resources
	if utils.IsOpenShift() {
		builder.Owns(&routev1.Route{})
	} else {
		builder.Owns(&networkingv1.Ingress{})
	}

	// Watch ClusterRoleBindings and map back to owning ClusterLogicPlatform by labels
	builder.Watches(
		&rbacv1.ClusterRoleBinding{},
		handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
			binding := obj.(*rbacv1.ClusterRoleBinding)
			name, hasName := binding.Labels["logic.kubesmarts.org/clusterplatform-name"]
			if hasName {
				return []ctrl.Request{
					{NamespacedName: types.NamespacedName{Name: name}},
				}
			}
			return nil
		}),
	)

	return builder.Complete(r)
}
