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

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LogicFlowRuntimeReconciler reconciles a LogicFlowRuntime object
type LogicFlowRuntimeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *LogicFlowRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rt logicv1.LogicFlowRuntime
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.applyDeployment(ctx, &rt); err != nil {
		log.Error(err, "failed to apply Deployment")
	}

	if err := r.applyService(ctx, &rt); err != nil {
		log.Error(err, "failed to apply Service")
	}

	if err := r.updateStatus(ctx, &rt); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicFlowRuntimeReconciler) applyConfigMap(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	// TODO: Should LogicFlowDefinition controller create those and apply to our deployment, or the other way around?
	return nil
}

func (r *LogicFlowRuntimeReconciler) applyDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	childLabels := ChildLabels(rt)
	deployment := appsv1ac.Deployment(rt.Name, rt.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(rt, logicv1.LogicFlowRuntimeKind)).
		WithSpec(
			ToDeploymentSpec(
				ContainerNameRunner,
				&rt.Spec.ApplicationSpec,
				childLabels,
				SelectorLabels(rt.Name),
				DefaultRunnerImage(rt.Spec.Persistence),
				WithPersistenceEnvVars(rt.Spec.Persistence, rt.Namespace),
				WithSecurityEnvVars(rt.Spec.Security),
				DefaultProbes(),
			),
		)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) applyService(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	svc := QuarkusService(rt, logicv1.LogicFlowRuntimeKind)

	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) updateStatus(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	rt.Status.ObservedGeneration = rt.Generation
	rt.Status.DeploymentRef.Name = rt.Name
	rt.Status.ServiceRef.Name = rt.Name
	rt.Status.Selector = labels.Set(SelectorLabels(rt.Name)).String()

	if err := r.updateStatusDeployment(ctx, rt); err != nil {
		return err
	}
	if err := r.updateStatusSvc(ctx, rt); err != nil {
		return err
	}

	rt.Status.Phase = logicv1.DerivePhase(rt.Status.Conditions, rt.Status.ReadyReplicas)

	// TODO DefinitionsRef, ConfigMapRef

	return r.Status().Update(ctx, rt)
}

func (r *LogicFlowRuntimeReconciler) updateStatusDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	var deployment appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(rt), &deployment)
	if apierrors.IsNotFound(err) {
		rt.Status.ReadyReplicas = 0
		rt.Status.Replicas = 0
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, logicv1.ReasonDeploymentNotFound, "")
		return nil
	}
	if err != nil {
		return err
	}

	rt.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	rt.Status.Replicas = deployment.Status.Replicas
	logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, logicv1.ReasonDeploymentProgressing, "")

	for _, cond := range deployment.Status.Conditions {
		switch cond.Type {
		case appsv1.DeploymentAvailable:
			if cond.Status == corev1.ConditionTrue {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionTrue, rt.Generation, cond.Reason, cond.Message)
			} else if cond.Status == corev1.ConditionFalse {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, cond.Reason, cond.Message)
			}
			break
		case appsv1.DeploymentProgressing:
			if cond.Status == corev1.ConditionFalse && cond.Reason == logicv1.ReasonProgressDeadlineExceeded {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, cond.Reason, cond.Message)
			}
			break
		}
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) updateStatusSvc(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	var svc corev1.Service
	err := r.Get(ctx, client.ObjectKeyFromObject(rt), &svc)
	if apierrors.IsNotFound(err) {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionServiceReady, metav1.ConditionFalse, rt.Generation, logicv1.ReasonServiceNotFound, "")
		return nil
	}
	if err != nil {
		return err
	}

	logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionServiceReady, metav1.ConditionTrue, rt.Generation, logicv1.ReasonReady, "")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogicFlowRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		For(&logicv1.LogicFlowRuntime{}).
		Named("logicflowruntime").
		Complete(r)
}
