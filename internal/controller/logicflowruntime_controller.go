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
	"k8s.io/client-go/util/retry"
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

type runtimePersistenceConfig struct {
	Runtime *logicv1.DatastoreSpec
	Quartz  *logicv1.DatastoreSpec
}

var errRuntimePersistenceNotConfigured = errors.New("runtime persistence is not configured")

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;delete

func (r *LogicFlowRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rt logicv1.LogicFlowRuntime
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Apply runtime defaults in the manager as well as admission so a runtime
	// created before the webhook is ready still gets its expected replica count.
	logicv1.ApplyLogicFlowRuntimeDefaults(&rt)

	configMaps, err := r.listConfigMaps(ctx, &rt)
	if err != nil {
		log.Error(err, "failed to list ConfigMaps")
		return ctrl.Result{}, err
	}

	eff, err := r.effectiveRuntimePersistence(ctx, &rt)
	if err != nil {
		log.Error(err, "failed to resolve effective runtime persistence")
		if !errors.Is(err, errRuntimePersistenceNotConfigured) {
			return ctrl.Result{}, err
		}

		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionPersistenceReady,
			metav1.ConditionFalse, rt.Generation,
			logicv1.ReasonRuntimePersistenceNotConfigured, err.Error())
		if statusErr := r.updateStatus(ctx, &rt, configMaps, nil); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil
	}

	persistenceMessage := "Platform runtime PostgreSQL datastore is configured"
	if eff != nil && eff.Quartz != nil {
		persistenceMessage += " with a separate Quartz datastore"
	}
	if logicv1.IsEphemeral(&rt) {
		persistenceMessage = "Runtime uses Ephemeral mode; no platform database is required"
	}
	logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionPersistenceReady,
		metav1.ConditionTrue, rt.Generation, logicv1.ReasonReady, persistenceMessage)

	if err := r.applyDeployment(ctx, &rt, configMaps, eff); err != nil {
		log.Error(err, "failed to apply Deployment")
		return ctrl.Result{}, err
	}

	if err := r.reconcilePodRBAC(ctx, &rt, persistenceDatastore(eff)); err != nil {
		log.Error(err, "failed to reconcile pod RBAC")
		return ctrl.Result{}, err
	}

	if eff != nil {
		platform, err := r.resolvePlatform(ctx, &rt)
		if err != nil {
			log.Error(err, "failed to resolve platform for DB secret replication")
			return ctrl.Result{}, err
		}
		if err := r.reconcileRuntimeDBSecret(ctx, &rt, eff.Runtime, platform); err != nil {
			log.Error(err, "failed to reconcile runtime DB secret")
			return ctrl.Result{}, err
		}
		if eff.Quartz != nil {
			if err := r.reconcileRuntimeDBSecret(ctx, &rt, eff.Quartz, platform); err != nil {
				log.Error(err, "failed to reconcile Quartz DB secret")
				return ctrl.Result{}, err
			}
		}
	}

	if err := r.reconcileLeases(ctx, &rt, persistenceDatastore(eff)); err != nil {
		log.Error(err, "failed to reconcile leases")
		return ctrl.Result{}, err
	}

	if err := r.applyService(ctx, &rt); err != nil {
		log.Error(err, "failed to apply Service")
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, &rt, configMaps, persistenceDatastore(eff)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicFlowRuntimeReconciler) listConfigMaps(ctx context.Context, rt *logicv1.LogicFlowRuntime) ([]corev1.ConfigMap, error) {
	var cmList corev1.ConfigMapList
	if err := r.List(ctx, &cmList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{logicv1.LabelRuntimeRef: rt.Name},
	); err != nil {
		return nil, err
	}
	sort.Slice(cmList.Items, func(i, j int) bool {
		return cmList.Items[i].Name < cmList.Items[j].Name
	})
	return cmList.Items, nil
}

func (r *LogicFlowRuntimeReconciler) applyDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap, eff *runtimePersistenceConfig) error {
	childLabels := ChildLabels(rt)
	var runtimeDatastore, quartzDatastore *logicv1.DatastoreSpec
	if eff != nil {
		runtimeDatastore = eff.Runtime
		quartzDatastore = eff.Quartz
	}
	opts := []ContainerOption{
		DefaultRunnerImage(runtimeDatastore),
		WithQuarkusPersistenceEnvVars(runtimeDatastore),
		WithQuarkusQuartzPersistenceEnvVars(quartzDatastore),
		WithSecurityEnvVars(rt.Spec.Security),
		WithStructuredLoggingEnvVars(rt.Spec.Logging),
		DefaultQuarkusProbes(),
		WithFlowSourcePath(),
		WithFlowVolumeMounts(configMaps),
	}
	if eff != nil {
		opts = append(opts, WithDurableEnvVars(rt), WithQuartzSchedulerEnvVars(rt), DurableStartupProbe())
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
	if eff != nil {
		spec.Template.Spec.WithServiceAccountName(rt.Name)
		spec.WithStrategy(durableDeploymentStrategy(effectiveReplicas(&rt.Spec.ApplicationSpec)))
	}
	deployment := appsv1ac.Deployment(rt.Name, rt.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(rt, logicv1.LogicFlowRuntimeKind)).
		WithSpec(spec)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) reconcileLeases(ctx context.Context, rt *logicv1.LogicFlowRuntime, eff *logicv1.DatastoreSpec) error {
	log := logf.FromContext(ctx)

	if eff == nil {
		return nil
	}

	desired := effectiveReplicas(&rt.Spec.ApplicationSpec)

	// Get deployment for owner references (only needed when creating new leases)
	var dep appsv1.Deployment
	var deploymentFound bool
	if err := r.Get(ctx, client.ObjectKeyFromObject(rt), &dep); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// Deployment not found - can't create leases (need it for owner reference)
		log.V(1).Info("Deployment not found, skipping new lease creation")
		deploymentFound = false
	} else {
		deploymentFound = true
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

	// Always create leases 0 to desired-1 (index-based)
	for i := range desired {
		name := fmt.Sprintf(LeaseMemberNameFmt, rt.Name, i)
		if _, ok := existing[name]; ok {
			continue
		}
		// Only create new leases if Deployment exists (needed for owner references)
		if !deploymentFound {
			log.V(1).Info("skipping new lease creation, deployment not found", "lease", name)
			continue
		}
		lease := newMemberLease(name, rt.Namespace, rt.Name, &dep)
		if err := r.Create(ctx, lease); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	// Leases persist across scale-down to avoid orphaning in-flight workflows.
	// They are only cleaned up when the runtime CR is deleted (via owner reference GC).

	return nil
}

func (r *LogicFlowRuntimeReconciler) reconcilePodRBAC(ctx context.Context, rt *logicv1.LogicFlowRuntime, eff *logicv1.DatastoreSpec) error {
	if eff == nil {
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

func (r *LogicFlowRuntimeReconciler) reconcileRuntimeDBSecret(ctx context.Context, rt *logicv1.LogicFlowRuntime, eff *logicv1.DatastoreSpec, platform *logicv1.ClusterLogicPlatform) error {
	log := logf.FromContext(ctx)

	if eff == nil || eff.PostgreSQL == nil {
		return nil
	}

	sourceSecretName := eff.PostgreSQL.SecretRef.Name
	sourceNamespace := platform.Spec.TargetNamespace

	// Same-namespace guard: if runtime is in the platform's target namespace,
	// the pod-local secret IS the source — skip replication to avoid GC loop.
	if rt.Namespace == sourceNamespace {
		return nil
	}

	// Get source secret from platform's target namespace
	var sourceSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Name: sourceSecretName, Namespace: sourceNamespace}, &sourceSecret); err != nil {
		if apierrors.IsNotFound(err) {
			log.Error(err, "source DB secret not found in platform namespace", "secret", sourceSecretName, "namespace", sourceNamespace)
			return err
		}
		return err
	}

	// Create owner reference for the runtime
	isController := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         logicv1.GroupVersion.String(),
		Kind:               logicv1.LogicFlowRuntimeKind,
		Name:               rt.Name,
		UID:                rt.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &isController,
	}

	// Create or update replica secret in runtime's namespace
	replicaSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            sourceSecretName,
			Namespace:       rt.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
			Labels: map[string]string{
				"logic.kubesmarts.org/managed-by": "logic-operator",
			},
		},
		Type: sourceSecret.Type,
		Data: sourceSecret.Data,
	}

	if err := r.Create(ctx, replicaSecret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		// Adopt-only-managed guard: only overwrite secrets already managed by logic-operator
		var existing corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Name: sourceSecretName, Namespace: rt.Namespace}, &existing); err != nil {
			return err
		}

		// Check if the existing secret is managed by logic-operator
		if existing.Labels == nil || existing.Labels["logic.kubesmarts.org/managed-by"] != "logic-operator" {
			// Unmanaged secret exists with same name — don't clobber it
			return fmt.Errorf("unmanaged secret %s/%s already exists; refusing to overwrite unmanaged resource (name collision)", rt.Namespace, sourceSecretName)
		}

		// Secret is already managed; update it
		existing.Data = sourceSecret.Data
		existing.Type = sourceSecret.Type
		existing.Labels["logic.kubesmarts.org/managed-by"] = "logic-operator"
		existing.OwnerReferences = []metav1.OwnerReference{ownerRef}
		if err := r.Update(ctx, &existing); err != nil {
			return err
		}
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) updateStatusLeases(ctx context.Context, rt *logicv1.LogicFlowRuntime, eff *logicv1.DatastoreSpec) error {
	if eff == nil {
		return nil
	}

	var leaseList coordinationv1.LeaseList
	if err := r.List(ctx, &leaseList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelDurablePool: rt.Name},
	); err != nil {
		return err
	}

	desired := effectiveReplicas(&rt.Spec.ApplicationSpec)
	rt.Status.LeaseReplicas = desired

	// Check if we have at least the desired number of leases (even if they exceed desired, that's OK for scale-down)
	if int32(len(leaseList.Items)) >= desired {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionLeaseReady, metav1.ConditionTrue, rt.Generation, logicv1.ReasonReady, "")
	} else {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionLeaseReady, metav1.ConditionFalse, rt.Generation, logicv1.ReasonLeaseNotFound,
			fmt.Sprintf("%d of %d leases ready", len(leaseList.Items), desired))
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) applyService(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	svc := QuarkusService(rt, logicv1.LogicFlowRuntimeKind)
	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) updateStatus(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap, eff *logicv1.DatastoreSpec) error {
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
	if err := r.updateStatusLeases(ctx, rt, eff); err != nil {
		return err
	}

	rt.Status.Phase = logicv1.DerivePhase(rt.Status.Conditions, rt.Status.ReadyReplicas)

	savedStatus := rt.Status.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, client.ObjectKeyFromObject(rt), rt); err != nil {
			return err
		}
		// Restore accumulated changes onto freshly fetched object
		rt.Status = *savedStatus
		return r.Status().Update(ctx, rt)
	})
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
		name := configMaps[i].Labels[logicv1.LabelWorkflowName]
		if name == "" {
			continue
		}
		defs = append(defs, logicv1.RuntimeDefinitionStatus{
			Name:    name,
			Version: configMaps[i].Labels[logicv1.LabelWorkflowVersion],
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
			switch cond.Status {
			case corev1.ConditionTrue:
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionTrue, rt.Generation, cond.Reason, cond.Message)
			case corev1.ConditionFalse:
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, cond.Reason, cond.Message)
			}
		case appsv1.DeploymentProgressing:
			if cond.Status == corev1.ConditionFalse && cond.Reason == logicv1.ReasonProgressDeadlineExceeded {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, cond.Reason, cond.Message)
			}
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

// resolvePlatform returns the ClusterLogicPlatform this runtime targets.
// Uses platformRef.Name when set; otherwise the single cluster platform.
func (r *LogicFlowRuntimeReconciler) resolvePlatform(ctx context.Context, rt *logicv1.LogicFlowRuntime) (*logicv1.ClusterLogicPlatform, error) {
	if rt.Spec.PlatformRef != nil && rt.Spec.PlatformRef.Name != "" {
		var clp logicv1.ClusterLogicPlatform
		if err := r.Get(ctx, client.ObjectKey{Name: rt.Spec.PlatformRef.Name}, &clp); err != nil {
			return nil, err
		}
		return &clp, nil
	}
	var list logicv1.ClusterLogicPlatformList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	switch len(list.Items) {
	case 0:
		return nil, fmt.Errorf("no ClusterLogicPlatform found; cannot resolve platform for runtime %s/%s", rt.Namespace, rt.Name)
	case 1:
		return &list.Items[0], nil
	default:
		return nil, fmt.Errorf("multiple ClusterLogicPlatforms found; set spec.platformRef.name on runtime %s/%s", rt.Namespace, rt.Name)
	}
}

// effectiveRuntimePersistence resolves the operational runtime datastore and an
// optional separate Quartz datastore: nil when ephemeral.
func (r *LogicFlowRuntimeReconciler) effectiveRuntimePersistence(ctx context.Context, rt *logicv1.LogicFlowRuntime) (*runtimePersistenceConfig, error) {
	if logicv1.IsEphemeral(rt) {
		return nil, nil
	}
	clp, err := r.resolvePlatform(ctx, rt)
	if err != nil {
		return nil, err
	}
	if clp.Spec.Runtime == nil || clp.Spec.Runtime.Persistence == nil {
		return nil, fmt.Errorf("%w: ClusterLogicPlatform %q must configure spec.runtime.persistence.runtime.postgresql, or set spec.persistenceMode to Ephemeral",
			errRuntimePersistenceNotConfigured, clp.Name)
	}
	runtimeDatastore := &clp.Spec.Runtime.Persistence.Runtime
	if runtimeDatastore.PostgreSQL == nil {
		return nil, fmt.Errorf("%w: ClusterLogicPlatform %q must configure spec.runtime.persistence.runtime.postgresql, or set spec.persistenceMode to Ephemeral",
			errRuntimePersistenceNotConfigured, clp.Name)
	}
	effective := &runtimePersistenceConfig{Runtime: runtimeDatastore}
	quartzDatastore := &clp.Spec.Runtime.Persistence.Quartz
	if quartzDatastore.PostgreSQL != nil {
		effective.Quartz = quartzDatastore
	}
	return effective, nil
}

func persistenceDatastore(eff *runtimePersistenceConfig) *logicv1.DatastoreSpec {
	if eff == nil {
		return nil
	}
	return eff.Runtime
}

func (r *LogicFlowRuntimeReconciler) mapConfigMapToRuntime(_ context.Context, obj client.Object) []reconcile.Request {
	rtName := obj.GetLabels()[logicv1.LabelRuntimeRef]
	if rtName == "" {
		return nil
	}
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: rtName, Namespace: obj.GetNamespace()}},
	}
}

func (r *LogicFlowRuntimeReconciler) mapPodToRuntime(_ context.Context, obj client.Object) []reconcile.Request {
	// Pods have selector labels (app.kubernetes.io/name=runtime-name)
	rtName := obj.GetLabels()[LabelKeyName]
	if rtName == "" {
		return nil
	}
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: rtName, Namespace: obj.GetNamespace()}},
	}
}

func runtimeRefLabelPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[logicv1.LabelRuntimeRef]
		return ok
	})
}

func (r *LogicFlowRuntimeReconciler) mapPlatformToRuntimes(ctx context.Context, _ client.Object) []reconcile.Request {
	// When ClusterLogicPlatform changes (or is created), enqueue all LogicFlowRuntimes
	var runtimeList logicv1.LogicFlowRuntimeList
	if err := r.List(ctx, &runtimeList); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for _, rt := range runtimeList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: rt.Name, Namespace: rt.Namespace},
		})
	}
	return requests
}

func (r *LogicFlowRuntimeReconciler) mapSecretToRuntimes(ctx context.Context, obj client.Object) []reconcile.Request {
	// Resolve platform database Secrets to every runtime that consumes that platform,
	// including runtimes in namespaces where the Secret is replicated.
	var platformList logicv1.ClusterLogicPlatformList
	if err := r.List(ctx, &platformList); err != nil {
		return nil
	}
	platformsByName := make(map[string]logicv1.ClusterLogicPlatform, len(platformList.Items))
	matchingPlatforms := make(map[string]struct{})
	for _, platform := range platformList.Items {
		platformsByName[platform.Name] = platform
		if platform.Spec.TargetNamespace != obj.GetNamespace() || platform.Spec.Runtime == nil || platform.Spec.Runtime.Persistence == nil {
			continue
		}
		persistence := platform.Spec.Runtime.Persistence
		if datastoreReferencesSecret(&persistence.Runtime, obj.GetName()) || datastoreReferencesSecret(&persistence.Quartz, obj.GetName()) {
			matchingPlatforms[platform.Name] = struct{}{}
		}
	}
	if len(matchingPlatforms) == 0 {
		return nil
	}

	var runtimeList logicv1.LogicFlowRuntimeList
	if err := r.List(ctx, &runtimeList); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for _, rt := range runtimeList.Items {
		if logicv1.IsEphemeral(&rt) {
			continue
		}
		platformName := ""
		if rt.Spec.PlatformRef != nil && rt.Spec.PlatformRef.Name != "" {
			platformName = rt.Spec.PlatformRef.Name
		} else if len(platformsByName) == 1 {
			for name := range platformsByName {
				platformName = name
			}
		}
		if _, matches := matchingPlatforms[platformName]; matches {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: rt.Name, Namespace: rt.Namespace}})
		}
	}
	return requests
}

func datastoreReferencesSecret(datastore *logicv1.DatastoreSpec, secretName string) bool {
	return datastore != nil && datastore.PostgreSQL != nil && datastore.PostgreSQL.SecretRef.Name == secretName
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
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.mapPodToRuntime),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(&logicv1.ClusterLogicPlatform{},
			handler.EnqueueRequestsFromMapFunc(r.mapPlatformToRuntimes),
		).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToRuntimes),
		).
		Named("logicflowruntime").
		Complete(r)
}
