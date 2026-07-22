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

	"k8s.io/apimachinery/pkg/runtime"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
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
	_ = logf.FromContext(ctx)

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

// SetupWithManager sets up the controller with the Manager.
func (r *LogicFlowRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowRuntime{}).
		Named("logicflowruntime").
		Complete(r)
}
