package controller

import (
	"context"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	routev1 "github.com/openshift/api/route/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// workflowGatewayComponent implements PlatformComponent for the Workflow Gateway service.
type workflowGatewayComponent struct {
	*LogicPlatformReconciler
}

func (c *workflowGatewayComponent) Name() string { return "Workflow Gateway" }
func (c *workflowGatewayComponent) Reconcile(ctx context.Context, plat *logicv1.LogicPlatform) error {
	return c.reconcileWorkflowGateway(ctx, plat)
}
func (c *workflowGatewayComponent) UpdateStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
	return c.updateWorkflowGatewayStatus(ctx, plat)
}

func (c *workflowGatewayComponent) reconcileWorkflowGateway(ctx context.Context, plat *logicv1.LogicPlatform) error {
	log := logf.FromContext(ctx)

	// Handle deletion: clean up resources before finalizer removal
	if plat.DeletionTimestamp != nil {
		if hasFinalizerWorkflowGateway(plat) {
			log.V(1).Info("LogicPlatform is being deleted, cleaning up gateway resources")
			return c.deleteWorkflowGatewayResources(ctx, plat)
		}
		return nil
	}

	// If gateway is disabled, clean up resources
	if plat.Spec.WorkflowGateway == nil {
		log.V(1).Info("WorkflowGateway is disabled, cleaning up resources")
		return c.deleteWorkflowGatewayResources(ctx, plat)
	}

	// Gateway enabled; add finalizer if not present
	if !hasFinalizerWorkflowGateway(plat) {
		original := plat.DeepCopy()
		plat.Finalizers = append(plat.Finalizers, FinalizerWorkflowGateway)
		if err := c.Patch(ctx, plat, client.MergeFrom(original)); err != nil {
			return err
		}
	}

	// Reconcile gateway resources in order
	if err := c.applyGatewayServiceAccount(ctx, plat); err != nil {
		return err
	}
	if err := c.applyGatewayClusterRoleBinding(ctx, plat); err != nil {
		return err
	}
	if err := c.applyGatewayDeployment(ctx, plat); err != nil {
		return err
	}
	if err := c.applyGatewayService(ctx, plat); err != nil {
		return err
	}
	if err := c.applyGatewayIngress(ctx, plat); err != nil {
		return err
	}

	log.V(1).Info("Gateway reconciliation complete", "platform", plat.Name)
	return nil
}

func (c *workflowGatewayComponent) applyGatewayServiceAccount(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	saKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}

	// Check if existing resource is owned by this platform
	var existing corev1.ServiceAccount
	if err := c.Get(ctx, saKey, &existing); err == nil && !hasOwner(&existing, plat) {
		return fmt.Errorf("ServiceAccount %s already exists and is not owned by this LogicPlatform", saKey)
	}

	sa := serviceAccountForGateway(plat)
	return c.Apply(ctx, sa, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *workflowGatewayComponent) applyGatewayClusterRoleBinding(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	bindingName := workflowGatewayClusterRoleBindingName(plat)

	// Build desired ClusterRoleBinding object
	desired := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
			Labels: map[string]string{
				"logic.kubesmarts.org/platform-namespace": plat.Namespace,
				"logic.kubesmarts.org/platform-name":      plat.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     ClusterRoleWorkflowGateway,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      gwName,
				Namespace: plat.Namespace,
			},
		},
	}

	// Check if binding exists and detect drift in immutable fields
	var existing rbacv1.ClusterRoleBinding
	bindingKey := client.ObjectKey{Name: bindingName}
	if err := c.Get(ctx, bindingKey, &existing); err == nil {
		// If owned by us and role reference drifted (immutable field), delete and recreate
		if hasOwner(&existing, plat) && existing.RoleRef != desired.RoleRef {
			if err := c.Delete(ctx, &existing); err != nil {
				return fmt.Errorf("failed to delete drifted ClusterRoleBinding: %w", err)
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Use Patch+Apply for ClusterRoleBinding
	//nolint:staticcheck
	return c.Patch(ctx, desired, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *workflowGatewayComponent) applyGatewayDeployment(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)

	deployKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	var existing appsv1.Deployment
	if err := c.Get(ctx, deployKey, &existing); err == nil && !hasOwner(&existing, plat) {
		return fmt.Errorf("deployment %s already exists and is not owned by this LogicPlatform", deployKey)
	}

	opts := []ContainerOption{
		WorkflowGatewayEnvVars(),
		WorkflowGatewayPorts(),
		WorkflowGatewayProbes(),
	}

	spec := ToDeploymentSpec(
		GatewayContainerName,
		&plat.Spec.WorkflowGateway.Application,
		childLabels,
		SelectorLabels(gwName),
		opts...,
	)

	// Do not set replicas if unset in CR — allow HPA to own scale.
	// But preserve replicas from PodTemplate if set there.
	if plat.Spec.WorkflowGateway.Application.Replicas == nil &&
		plat.Spec.WorkflowGateway.Application.PodTemplate.Replicas == nil {
		spec.Replicas = nil
	}

	// Set dedicated ServiceAccount
	spec.Template.Spec.ServiceAccountName = &gwName

	deployment := appsv1ac.Deployment(gwName, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return c.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *workflowGatewayComponent) applyGatewayService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)

	svcKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	var existing corev1.Service
	if err := c.Get(ctx, svcKey, &existing); err == nil && !hasOwner(&existing, plat) {
		return fmt.Errorf("service %s already exists and is not owned by this LogicPlatform", svcKey)
	}

	spec := serviceForGateway(plat).Spec

	service := corev1ac.Service(gwName, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return c.Apply(ctx, service, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *workflowGatewayComponent) applyGatewayIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// If ingress disabled, delete any existing resources
	if plat.Spec.WorkflowGateway.Ingress == nil || !plat.Spec.WorkflowGateway.Ingress.Enabled {
		return c.deleteGatewayIngress(ctx, plat)
	}

	// Apply Ingress or Route based on platform
	if utils.IsOpenShift() {
		return c.applyGatewayRoute(ctx, plat)
	}
	return c.applyGatewayKubernetesIngress(ctx, plat)
}

func (c *workflowGatewayComponent) deleteGatewayIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)

	// Delete Ingress if owned by this platform
	ingKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	var ing networkingv1.Ingress
	if err := c.Get(ctx, ingKey, &ing); err == nil {
		if hasOwner(&ing, plat) {
			if err := c.Delete(ctx, &ing); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete Route if owned by this platform
	if utils.IsOpenShift() {
		rtKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
		var rt routev1.Route
		if err := c.Get(ctx, rtKey, &rt); err == nil {
			if hasOwner(&rt, plat) {
				if err := c.Delete(ctx, &rt); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (c *workflowGatewayComponent) applyGatewayKubernetesIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	ingress := ingressForGateway(plat)
	return c.Apply(ctx, ingress, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *workflowGatewayComponent) applyGatewayRoute(ctx context.Context, plat *logicv1.LogicPlatform) error {
	route := routeForGateway(plat)
	//nolint:staticcheck
	return c.Patch(ctx, route, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *workflowGatewayComponent) deleteWorkflowGatewayResources(ctx context.Context, plat *logicv1.LogicPlatform) error {
	log := logf.FromContext(ctx)
	gwName := workflowGatewayName(plat)
	bindingName := workflowGatewayClusterRoleBindingName(plat)

	// Delete Ingress/Route
	if err := c.deleteGatewayIngress(ctx, plat); err != nil {
		return err
	}

	// Delete Service (check ownership first)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: plat.Namespace},
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(svc), svc); err == nil {
		// Service exists; verify it's owned by this platform
		if !hasOwner(svc, plat) {
			return fmt.Errorf("cannot delete Service %s/%s: not owned by LogicPlatform %s/%s", svc.Namespace, svc.Name, plat.Namespace, plat.Name)
		}
		if err := c.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete Deployment (check ownership first)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: plat.Namespace},
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(deploy), deploy); err == nil {
		if !hasOwner(deploy, plat) {
			return fmt.Errorf("cannot delete Deployment %s/%s: not owned by LogicPlatform %s/%s", deploy.Namespace, deploy.Name, plat.Namespace, plat.Name)
		}
		if err := c.Delete(ctx, deploy); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ClusterRoleBinding (check labels first)
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: bindingName},
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(binding), binding); err == nil {
		// Verify binding is labeled with this platform
		if binding.Labels["logic.kubesmarts.org/platform-namespace"] != plat.Namespace ||
			binding.Labels["logic.kubesmarts.org/platform-name"] != plat.Name {
			return fmt.Errorf("cannot delete ClusterRoleBinding %s: not labeled for LogicPlatform %s/%s", bindingName, plat.Namespace, plat.Name)
		}
		if err := c.Delete(ctx, binding); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ServiceAccount (check ownership first)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: plat.Namespace},
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(sa), sa); err == nil {
		if !hasOwner(sa, plat) {
			return fmt.Errorf("cannot delete ServiceAccount %s/%s: not owned by LogicPlatform %s/%s", sa.Namespace, sa.Name, plat.Namespace, plat.Name)
		}
		if err := c.Delete(ctx, sa); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Remove finalizer only if it was present
	if hasFinalizerWorkflowGateway(plat) {
		original := plat.DeepCopy()
		removeFinalizerWorkflowGateway(plat)
		if err := c.Patch(ctx, plat, client.MergeFrom(original)); err != nil {
			return err
		}
	}

	log.V(1).Info("Gateway resources cleaned up", "platform", plat.Name)
	return nil
}

func (c *workflowGatewayComponent) updateWorkflowGatewayStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// If gateway is disabled, clear status
	if plat.Spec.WorkflowGateway == nil {
		plat.Status.WorkflowGateway = nil
		removeLogicPlatformCondition(&plat.Status.Conditions, logicv1.ConditionWorkflowGatewayReady)
		return nil
	}

	gwName := workflowGatewayName(plat)
	status := &logicv1.WorkflowGatewayStatus{
		DeploymentRef:     corev1.LocalObjectReference{Name: gwName},
		ServiceRef:        corev1.LocalObjectReference{Name: gwName},
		ServiceAccountRef: corev1.LocalObjectReference{Name: gwName},
	}

	// Get Deployment and check readiness.
	var deploy appsv1.Deployment
	deployKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	deploymentFound := false
	deploymentReady := false
	reason := logicv1.ReasonDeploymentNotFound
	message := "Workflow Gateway Deployment not found"
	if err := c.Get(ctx, deployKey, &deploy); err == nil {
		deploymentFound = true
		if deploy.Spec.Replicas != nil {
			status.Replicas.Desired = *deploy.Spec.Replicas
		}
		status.Replicas.Current = deploy.Status.Replicas
		status.Replicas.Ready = deploy.Status.ReadyReplicas
		status.Replicas.Updated = deploy.Status.UpdatedReplicas
		deploymentReady = deploy.Status.ReadyReplicas > 0
		if deploymentReady {
			reason = logicv1.ReasonReady
			message = "Workflow Gateway Deployment is ready"
		} else {
			reason = logicv1.ReasonDeploymentProgressing
			message = "Workflow Gateway Deployment has no ready replicas"
			for _, condition := range deploy.Status.Conditions {
				if condition.Type == appsv1.DeploymentProgressing &&
					condition.Status == corev1.ConditionFalse &&
					condition.Reason == logicv1.ReasonProgressDeadlineExceeded {
					reason = logicv1.ReasonProgressDeadlineExceeded
					message = condition.Message
					break
				}
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Get Service.
	serviceFound := false
	var svc corev1.Service
	svcKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	if err := c.Get(ctx, svcKey, &svc); err == nil {
		serviceFound = true
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	status.Ready = deploymentFound && deploymentReady && serviceFound
	if status.Ready {
		reason = logicv1.ReasonReady
		message = "Workflow Gateway is ready"
	} else if deploymentFound && deploymentReady && !serviceFound {
		reason = logicv1.ReasonServiceNotFound
		message = "Workflow Gateway Service not found"
	}
	conditionStatus := metav1.ConditionFalse
	if status.Ready {
		conditionStatus = metav1.ConditionTrue
	}
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionWorkflowGatewayReady,
		conditionStatus, plat.Generation, reason, message)

	// Get Ingress/Route URL if configured
	if plat.Spec.WorkflowGateway.Ingress != nil && plat.Spec.WorkflowGateway.Ingress.Enabled {
		ingressKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
		url, err := ResolveIngressURL(ctx, c.Client, ingressKey, plat.Spec.WorkflowGateway.Ingress.TLS.Enabled)
		if err != nil {
			return err
		}
		status.URL = url

		// Populate Ingress/Route ref
		if utils.IsOpenShift() {
			status.RouteRef = &corev1.LocalObjectReference{Name: gwName}
			status.IngressRef = nil
		} else {
			status.IngressRef = &corev1.LocalObjectReference{Name: gwName}
			status.RouteRef = nil
		}
	}

	plat.Status.WorkflowGateway = status
	return nil
}
