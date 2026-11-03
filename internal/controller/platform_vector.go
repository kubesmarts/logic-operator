package controller

import (
	"context"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

// vectorComponent implements PlatformComponent for the Vector service.
type vectorComponent struct {
	*LogicPlatformReconciler
}

func (c *vectorComponent) Name() string { return "Vector" }
func (c *vectorComponent) Reconcile(ctx context.Context, plat *logicv1.LogicPlatform) error {
	return c.reconcileVector(ctx, plat)
}
func (c *vectorComponent) UpdateStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
	return c.updateVectorStatus(ctx, plat)
}

func (c *vectorComponent) reconcileVector(ctx context.Context, plat *logicv1.LogicPlatform) error {
	if plat.Spec.DataIndex.Vector == nil || !plat.Spec.DataIndex.Vector.Enabled {
		return c.deleteVectorResources(ctx, plat)
	}

	if err := c.reconcileVectorRBAC(ctx, plat); err != nil {
		return err
	}
	if err := c.applyVectorConfigMap(ctx, plat); err != nil {
		return err
	}
	if err := c.applyVectorDaemonSet(ctx, plat); err != nil {
		return err
	}
	if err := c.applyVectorService(ctx, plat); err != nil {
		return err
	}
	return nil
}

func (c *vectorComponent) applyVectorDaemonSet(ctx context.Context, plat *logicv1.LogicPlatform) error {
	childLabels := ChildLabelsInstance(plat, vectorName(plat))
	vectorSelector := SelectorLabels(vectorName(plat))
	// Merge selector labels into pod labels so they match
	vectorLabels := MergeMaps(childLabels, vectorSelector)

	containerOpts := []ContainerOption{
		WithVectorEnvVars(plat),
		WithVectorVolumeMounts(),
		VectorProbes(),
	}

	spec := ToDaemonSetSpec(
		ContainerNameVector,
		&plat.Spec.DataIndex.Vector.Application,
		vectorLabels,
		vectorSelector,
		containerOpts,
		WithoutRestrictedSecurity(),
	)
	spec.Template.Spec.
		WithVolumes(VectorVolumes(plat)...).
		WithServiceAccountName(vectorName(plat)).
		WithTolerations(corev1ac.Toleration().
			WithKey("node-role.kubernetes.io/control-plane").
			WithOperator(corev1.TolerationOpExists).
			WithEffect(corev1.TaintEffectNoSchedule),
			corev1ac.Toleration().
				WithKey("node-role.kubernetes.io/control-master").
				WithOperator(corev1.TolerationOpExists).
				WithEffect(corev1.TaintEffectNoSchedule))

	daemonSet := appsv1ac.DaemonSet(vectorName(plat), plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return c.Apply(ctx, daemonSet, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *vectorComponent) applyVectorService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	childLabels := ChildLabelsInstance(plat, vectorName(plat))
	vectorSelector := SelectorLabels(vectorName(plat))

	svc := corev1ac.Service(vectorName(plat), plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(
			corev1ac.ServiceSpec().
				WithSelector(vectorSelector).
				WithPorts(
					corev1ac.ServicePort().
						WithName("api").
						WithPort(VectorAPIPort).
						WithProtocol(corev1.ProtocolTCP).
						WithTargetPort(intstr.FromInt32(VectorAPIPort)),
					corev1ac.ServicePort().
						WithName("metrics").
						WithPort(VectorPort).
						WithProtocol(corev1.ProtocolTCP).
						WithTargetPort(intstr.FromInt32(VectorPort)),
				),
		)

	return c.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *vectorComponent) reconcileVectorRBAC(ctx context.Context, plat *logicv1.LogicPlatform) error {
	rbacName := vectorName(plat)
	childLabels := ChildLabelsInstance(plat, rbacName)
	isController := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         logicv1.GroupVersion.String(),
		Kind:               logicv1.LogicPlatformKind,
		Name:               plat.Name,
		UID:                plat.UID,
		Controller:         &isController,
		BlockOwnerDeletion: &isController,
	}
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rbacName,
			Namespace:       plat.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
			Labels:          childLabels,
		},
	}
	if err := c.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	rb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rbacName,
			Namespace:       plat.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
			Labels:          childLabels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     ClusterRoleVector,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      rbacName,
				Namespace: plat.Namespace,
			},
		},
	}
	if err := c.Create(ctx, rb); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (c *vectorComponent) applyVectorConfigMap(ctx context.Context, plat *logicv1.LogicPlatform) error {
	cmName := vectorName(plat)
	childLabels := ChildLabelsInstance(plat, cmName)
	// TODO: once we add support to ES, do the check here for which config to create
	cm := corev1ac.ConfigMap(cmName, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithData(map[string]string{"vector.yaml": vectorPostgreSQLConfig})

	return c.Apply(ctx, cm, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *vectorComponent) deleteVectorResources(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// Delete DaemonSet
	if err := c.Delete(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete Service
	if err := c.Delete(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ConfigMap
	// TODO: once we add support to ES, do the check here for which config to create
	if err := c.Delete(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ServiceAccount
	if err := c.Delete(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ClusterRoleBinding
	if err := c.Delete(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat)},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (c *vectorComponent) updateVectorStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// If Vector is disabled, clear status
	if plat.Spec.DataIndex.Vector == nil || !plat.Spec.DataIndex.Vector.Enabled {
		plat.Status.Vector = nil
		removeLogicPlatformCondition(&plat.Status.Conditions, logicv1.ConditionVectorReady)
		return nil
	}

	// Initialize status if nil
	if plat.Status.Vector == nil {
		plat.Status.Vector = &logicv1.VectorStatus{}
	}

	// Fetch DaemonSet
	var ds appsv1.DaemonSet
	err := c.Get(ctx, client.ObjectKey{Name: vectorName(plat), Namespace: plat.Namespace}, &ds)
	if apierrors.IsNotFound(err) {
		plat.Status.Vector.Ready = false
		plat.Status.Vector.DaemonSetRef = corev1.LocalObjectReference{Name: vectorName(plat)}
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionVectorReady,
			metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonDaemonSetNotFound, "Vector DaemonSet not found")
		return nil
	}
	if err != nil {
		return err
	}

	// Update DaemonSetRef
	plat.Status.Vector.DaemonSetRef = corev1.LocalObjectReference{Name: ds.Name}

	// Check if DaemonSet is ready (all desired pods are ready)
	isReady := ds.Status.DesiredNumberScheduled == ds.Status.NumberReady && ds.Status.DesiredNumberScheduled > 0
	plat.Status.Vector.Ready = isReady

	// Set MetricsEndpoint
	plat.Status.Vector.MetricsEndpoint = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d/metrics",
		vectorName(plat), plat.Namespace, VectorPort,
	)

	// Set condition based on ready status
	if isReady {
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionVectorReady,
			metav1.ConditionTrue, plat.Generation,
			logicv1.ReasonReady, "Vector DaemonSet is ready")
	} else {
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionVectorReady,
			metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonDaemonSetProgressing,
			fmt.Sprintf("Vector DaemonSet: %d/%d nodes ready", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled))
	}

	return nil
}
