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
	"sort"

	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type LogicFlowRuntimeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;delete

func (r *LogicFlowRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rt logicv1.LogicFlowRuntime
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	configMaps, err := r.listConfigMaps(ctx, &rt)
	if err != nil {
		log.Error(err, "failed to list ConfigMaps")
		return ctrl.Result{}, err
	}

	if err := r.applyDeployment(ctx, &rt, configMaps); err != nil {
		log.Error(err, "failed to apply Deployment")
	}

	if err := r.reconcilePodRBAC(ctx, &rt); err != nil {
		log.Error(err, "failed to reconcile pod RBAC")
	}

	if err := r.reconcileLeases(ctx, &rt); err != nil {
		log.Error(err, "failed to reconcile leases")
	}

	if err := r.applyService(ctx, &rt); err != nil {
		log.Error(err, "failed to apply Service")
	}

	if err := r.updateStatus(ctx, &rt, configMaps); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicFlowRuntimeReconciler) listConfigMaps(ctx context.Context, rt *logicv1.LogicFlowRuntime) ([]corev1.ConfigMap, error) {
	var cmList corev1.ConfigMapList
	if err := r.List(ctx, &cmList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelRuntimeRef: rt.Name},
	); err != nil {
		return nil, err
	}
	sort.Slice(cmList.Items, func(i, j int) bool {
		return cmList.Items[i].Name < cmList.Items[j].Name
	})
	return cmList.Items, nil
}

func (r *LogicFlowRuntimeReconciler) applyDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap) error {
	childLabels := ChildLabels(rt)
	opts := []ContainerOption{
		DefaultRunnerImage(rt.Spec.Persistence),
		WithPersistenceEnvVars(rt.Spec.Persistence, rt.Namespace),
		WithSecurityEnvVars(rt.Spec.Security),
		DefaultProbes(),
		WithFlowSourcePath(),
		WithFlowVolumeMounts(configMaps),
	}
	if rt.Spec.Persistence != nil {
		opts = append(opts, WithDurableEnvVars(rt))
	}
	spec := ToDeploymentSpec(
		ContainerNameRunner,
		&rt.Spec.ApplicationSpec,
		childLabels,
		SelectorLabels(rt.Name),
		opts...,
	)
	if len(configMaps) > 0 {
		spec.Template.Spec.WithVolumes(FlowVolumes(configMaps)...)
	}
	if rt.Spec.Persistence != nil {
		spec.Template.Spec.WithServiceAccountName(rt.Name)
	}
	deployment := appsv1ac.Deployment(rt.Name, rt.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(rt, logicv1.LogicFlowRuntimeKind)).
		WithSpec(spec)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) reconcileLeases(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	if rt.Spec.Persistence == nil {
		return nil
	}

	desired := effectiveReplicas(&rt.Spec.ApplicationSpec)

	var dep appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(rt), &dep); err != nil {
		return err
	}

	var leaseList coordinationv1.LeaseList
	if err := r.List(ctx, &leaseList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelDurablePool: rt.Name},
	); err != nil {
		return err
	}

	existing := make(map[string]struct{}, len(leaseList.Items))
	for i := range leaseList.Items {
		existing[leaseList.Items[i].Name] = struct{}{}
	}

	desiredNames := make(map[string]struct{}, desired)
	for i := int32(0); i < desired; i++ {
		name := fmt.Sprintf(LeaseMemberNameFmt, rt.Name, i)
		desiredNames[name] = struct{}{}
		if _, ok := existing[name]; ok {
			continue
		}
		lease := newMemberLease(name, rt.Namespace, rt.Name, &dep)
		if err := r.Create(ctx, lease); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	for i := range leaseList.Items {
		if _, ok := desiredNames[leaseList.Items[i].Name]; !ok {
			if err := r.Delete(ctx, &leaseList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) reconcilePodRBAC(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	if rt.Spec.Persistence == nil {
		return nil
	}

	isController := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         logicv1.GroupVersion.String(),
		Kind:               logicv1.LogicFlowRuntimeKind,
		Name:               rt.Name,
		UID:                rt.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &isController,
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rt.Name,
			Namespace:       rt.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	if err := r.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rt.Name + "-durable",
			Namespace:       rt.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     ClusterRoleDurable,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      rt.Name,
				Namespace: rt.Namespace,
			},
		},
	}
	if err := r.Create(ctx, rb); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) updateStatusLeases(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	if rt.Spec.Persistence == nil {
		return nil
	}

	var leaseList coordinationv1.LeaseList
	if err := r.List(ctx, &leaseList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelDurablePool: rt.Name},
	); err != nil {
		return err
	}

	rt.Status.LeaseReplicas = int32(len(leaseList.Items))
	desired := effectiveReplicas(&rt.Spec.ApplicationSpec)

	if rt.Status.LeaseReplicas >= desired {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionLeaseReady, metav1.ConditionTrue, rt.Generation, logicv1.ReasonReady, "")
	} else {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionLeaseReady, metav1.ConditionFalse, rt.Generation, logicv1.ReasonLeaseNotFound,
			fmt.Sprintf("%d of %d leases ready", rt.Status.LeaseReplicas, desired))
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) applyService(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	svc := QuarkusService(rt, logicv1.LogicFlowRuntimeKind)
	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) updateStatus(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap) error {
	rt.Status.ObservedGeneration = rt.Generation
	rt.Status.DeploymentRef.Name = rt.Name
	rt.Status.ServiceRef.Name = rt.Name
	rt.Status.Selector = labels.Set(SelectorLabels(rt.Name)).String()

	rt.Status.ConfigMapRefs = configMapRefs(configMaps)
	rt.Status.Definitions = definitionsFromConfigMaps(configMaps)

	if err := r.updateStatusDeployment(ctx, rt); err != nil {
		return err
	}
	if err := r.updateStatusSvc(ctx, rt); err != nil {
		return err
	}
	if err := r.updateStatusLeases(ctx, rt); err != nil {
		return err
	}

	rt.Status.Phase = logicv1.DerivePhase(rt.Status.Conditions, rt.Status.ReadyReplicas)

	return r.Status().Update(ctx, rt)
}

func configMapRefs(configMaps []corev1.ConfigMap) []corev1.LocalObjectReference {
	refs := make([]corev1.LocalObjectReference, 0, len(configMaps))
	for i := range configMaps {
		refs = append(refs, corev1.LocalObjectReference{Name: configMaps[i].Name})
	}
	return refs
}

func definitionsFromConfigMaps(configMaps []corev1.ConfigMap) []logicv1.RuntimeDefinitionStatus {
	defs := make([]logicv1.RuntimeDefinitionStatus, 0, len(configMaps))
	for i := range configMaps {
		name := configMaps[i].Labels[LabelWorkflowName]
		if name == "" {
			continue
		}
		defs = append(defs, logicv1.RuntimeDefinitionStatus{
			Name:    name,
			Version: configMaps[i].Labels[LabelWorkflowVersion],
		})
	}
	return defs
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

func (r *LogicFlowRuntimeReconciler) mapConfigMapToRuntime(ctx context.Context, obj client.Object) []reconcile.Request {
	rtName := obj.GetLabels()[LabelRuntimeRef]
	if rtName == "" {
		return nil
	}
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: rtName, Namespace: obj.GetNamespace()}},
	}
}

func runtimeRefLabelPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[LabelRuntimeRef]
		return ok
	})
}

func (r *LogicFlowRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowRuntime{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToRuntime),
			builder.WithPredicates(runtimeRefLabelPredicate()),
		).
		Named("logicflowruntime").
		Complete(r)
}
