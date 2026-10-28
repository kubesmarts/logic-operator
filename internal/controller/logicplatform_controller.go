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
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// LogicPlatformReconciler reconciles a LogicPlatform object
type LogicPlatformReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	DatabaseConnector DatabaseConnector
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms/finalizers,verbs=update
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
// the LogicPlatform object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *LogicPlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	var plat logicv1.LogicPlatform
	if err := r.Get(ctx, req.NamespacedName, &plat); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.reconcileDataIndex(ctx, &plat); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileVector(ctx, &plat); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileWorkflowGateway(ctx, &plat); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.updateStatus(ctx, &plat); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicPlatformReconciler) reconcileDataIndex(ctx context.Context, plat *logicv1.LogicPlatform) error {
	log := logf.FromContext(ctx)

	if !plat.Spec.DataIndex.Enabled {
		log.V(1).Info("DataIndex is disabled, skipping reconciliation")
		return nil
	}

	if err := r.applyDataIndexDeployment(ctx, plat); err != nil {
		return err
	}
	if err := r.applyDataIndexService(ctx, plat); err != nil {
		return err
	}
	// Apply or delete Ingress/Route based on enabled flag
	if err := r.applyDataIndexIngress(ctx, plat); err != nil {
		return err
	}
	return nil
}

func (r *LogicPlatformReconciler) applyDataIndexDeployment(ctx context.Context, plat *logicv1.LogicPlatform) error {
	childLabels := ChildLabelsInstance(plat, dataIndexName(plat))
	opts := []ContainerOption{
		WithQuarkusPersistenceEnvVars(plat.Spec.DataIndex.Persistence),
		WithFlywayPersistenceVars(),
		WithGraphQLVars(),
		DefaultQuarkusProbes(),
	}
	spec := ToDeploymentSpec(
		ContainerNameDataIndex,
		&plat.Spec.DataIndex.Application,
		childLabels,
		SelectorLabels(dataIndexName(plat)),
		opts...,
	)
	deployment := appsv1ac.Deployment(dataIndexName(plat), plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyDataIndexService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	diName := dataIndexName(plat)
	svc := QuarkusServiceWithSelectorAndName(plat, logicv1.LogicPlatformKind, SelectorLabels(diName), diName)
	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyDataIndexIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// If ingress is not enabled, delete any existing Ingress/Route resources
	if plat.Spec.DataIndex.Ingress == nil || !plat.Spec.DataIndex.Ingress.Enabled {
		return r.deleteDataIndexIngress(ctx, plat)
	}

	// Apply Ingress/Route based on platform
	if utils.IsOpenShift() {
		return r.applyDataIndexRoute(ctx, plat)
	}
	return r.applyDataIndexKubernetesIngress(ctx, plat)
}

func (r *LogicPlatformReconciler) deleteDataIndexIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// Clear status references
	plat.Status.IngressRef = nil
	plat.Status.RouteRef = nil

	// Delete Kubernetes Ingress if it exists
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dataIndexName(plat),
			Namespace: plat.Namespace,
		},
	}
	if err := r.Delete(ctx, ingress); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete OpenShift Route if it exists
	if utils.IsOpenShift() {
		route := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dataIndexName(plat),
				Namespace: plat.Namespace,
			},
		}
		if err := r.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (r *LogicPlatformReconciler) applyDataIndexKubernetesIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	desired := ingressForDataIndex(plat)
	plat.Status.IngressRef = &corev1.LocalObjectReference{Name: *desired.Name}
	plat.Status.RouteRef = nil
	return r.Apply(ctx, desired, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyDataIndexRoute(ctx context.Context, plat *logicv1.LogicPlatform) error {
	desired := routeForDataIndex(plat)
	plat.Status.RouteRef = &corev1.LocalObjectReference{Name: desired.Name}
	plat.Status.IngressRef = nil
	//nolint:staticcheck // OpenShift Route API has no apply configurations; deprecated Patch+client.Apply is the only option
	return r.Patch(ctx, desired, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) updateDataIndexStatusDeployment(ctx context.Context, plat *logicv1.LogicPlatform) error {
	var deployment appsv1.Deployment
	err := r.Get(ctx, objectKeyDataIndex(plat), &deployment)
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

func (r *LogicPlatformReconciler) updateDeploymentStatusService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	var svc corev1.Service
	err := r.Get(ctx, objectKeyDataIndex(plat), &svc)
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
		dataIndexName(plat), plat.Namespace, defaultPort)
}

func (r *LogicPlatformReconciler) resolveExternalURL(ctx context.Context, plat *logicv1.LogicPlatform) (string, error) {
	return ResolveIngressURL(ctx, r.Client, objectKeyDataIndex(plat), plat.Spec.DataIndex.Ingress.TLS.Enabled)
}

func (r *LogicPlatformReconciler) updateStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
	diName := dataIndexName(plat)
	// Set observed generation
	plat.Status.ObservedGeneration = plat.Generation

	if plat.Spec.DataIndex.Enabled {
		if plat.Status.DataIndex == nil {
			plat.Status.DataIndex = &logicv1.DataIndexStatus{}
		}

		// Set resource references
		plat.Status.DataIndex.Service.DeploymentRef.Name = diName
		plat.Status.DataIndex.Service.ServiceRef.Name = diName

		// Update persistence, deployment, and service status.
		if err := r.updateStatusPersistence(ctx, plat); err != nil {
			return err
		}
		if err := r.updateDataIndexStatusDeployment(ctx, plat); err != nil {
			return err
		}
		if err := r.updateDeploymentStatusService(ctx, plat); err != nil {
			return err
		}

		ready, reason, message := platformComponentReadiness(plat.Status.Conditions,
			logicv1.ConditionDataIndexDeploymentAvailable,
			logicv1.ConditionDataIndexServiceReady,
			logicv1.ConditionDataIndexPersistenceReady,
		)
		plat.Status.DataIndex.Ready = ready
		if ready {
			reason = logicv1.ReasonReady
			message = "Data Index is ready"
		}
		conditionStatus := metav1.ConditionFalse
		if ready {
			conditionStatus = metav1.ConditionTrue
		}
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexReady,
			conditionStatus, plat.Generation, reason, message)
	} else {
		plat.Status.DataIndex = nil
		removeLogicPlatformCondition(&plat.Status.Conditions,
			logicv1.ConditionDataIndexReady,
			logicv1.ConditionDataIndexDeploymentAvailable,
			logicv1.ConditionDataIndexServiceReady,
			logicv1.ConditionDataIndexPersistenceReady,
		)
	}

	// Update Vector status independently so each enabled component has a status
	// entry at the same level as Data Index and Workflow Gateway.
	if err := r.updateVectorStatus(ctx, plat); err != nil {
		return err
	}

	// Update WorkflowGateway status
	if err := r.updateWorkflowGatewayStatus(ctx, plat); err != nil {
		return err
	}

	updateLogicPlatformReadiness(plat)

	// Update status in API server with retry on conflict
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

func platformComponentReadiness(conditions []metav1.Condition, conditionTypes ...string) (bool, string, string) {
	for _, conditionType := range conditionTypes {
		condition := logicv1.FindCondition(conditions, conditionType)
		if condition == nil {
			return false, logicv1.ReasonComponentsNotReady, fmt.Sprintf("%s condition is not available", conditionType)
		}
		if condition.Status != metav1.ConditionTrue {
			reason := condition.Reason
			if reason == "" {
				reason = logicv1.ReasonComponentsNotReady
			}
			return false, reason, condition.Message
		}
	}
	return true, logicv1.ReasonReady, ""
}

func updateLogicPlatformReadiness(plat *logicv1.LogicPlatform) {
	var notReady []string
	components := 0
	if plat.Spec.DataIndex.Enabled {
		components++
		if plat.Status.DataIndex == nil || !plat.Status.DataIndex.Ready {
			notReady = append(notReady, "Data Index")
		}
	}
	if plat.Spec.DataIndex.Vector != nil && plat.Spec.DataIndex.Vector.Enabled {
		components++
		if plat.Status.Vector == nil || !plat.Status.Vector.Ready {
			notReady = append(notReady, "Vector")
		}
	}
	if plat.Spec.WorkflowGateway != nil {
		components++
		if plat.Status.WorkflowGateway == nil || !plat.Status.WorkflowGateway.Ready {
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
		plat.Status.Phase = logicv1.LogicPlatformStatusPhase(logicv1.ApplicationPhaseReady)
		return
	}

	phase := logicv1.ApplicationPhasePending
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
			phase = logicv1.ApplicationPhaseFailed
			reason = logicv1.ReasonProgressDeadlineExceeded
			break
		}
	}
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionReady,
		metav1.ConditionFalse, plat.Generation, reason,
		fmt.Sprintf("Waiting for enabled platform components: %s", strings.Join(notReady, ", ")))
	plat.Status.Phase = logicv1.LogicPlatformStatusPhase(phase)
}

func removeLogicPlatformCondition(conditions *[]metav1.Condition, conditionTypes ...string) {
	remove := make(map[string]struct{}, len(conditionTypes))
	for _, conditionType := range conditionTypes {
		remove[conditionType] = struct{}{}
	}

	filtered := (*conditions)[:0]
	for _, condition := range *conditions {
		if _, found := remove[condition.Type]; !found {
			filtered = append(filtered, condition)
		}
	}
	*conditions = filtered
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

	// Check if service exists
	var svc corev1.Service
	svcNamespace := pg.ServiceRef.Namespace
	if svcNamespace == "" {
		svcNamespace = plat.Namespace
	}
	svcKey := client.ObjectKey{
		Name:      pg.ServiceRef.Name,
		Namespace: svcNamespace,
	}
	err = r.Get(ctx, svcKey, &svc)
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

	plat.Status.DataIndex.Persistence = status

	// Set condition based on validation
	if status.Valid {
		// Configuration is valid, check database connectivity
		healthy, reason, message := r.checkPersistenceConnectivity(ctx, plat)
		status.DatabaseConnected = &healthy
		plat.Status.DataIndex.Persistence = status

		if healthy {
			logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
				metav1.ConditionTrue, plat.Generation,
				reason, message)
		} else {
			logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
				metav1.ConditionFalse, plat.Generation,
				reason, message)
		}
	} else {
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
			metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonPersistenceConfigInvalid, status.Error)
	}

	return nil
}

func (r *LogicPlatformReconciler) reconcileVector(ctx context.Context, plat *logicv1.LogicPlatform) error {
	if plat.Spec.DataIndex.Vector == nil || !plat.Spec.DataIndex.Vector.Enabled {
		return r.deleteVectorResources(ctx, plat)
	}

	if err := r.reconcileVectorRBAC(ctx, plat); err != nil {
		return err
	}
	if err := r.applyVectorConfigMap(ctx, plat); err != nil {
		return err
	}
	if err := r.applyVectorDaemonSet(ctx, plat); err != nil {
		return err
	}
	if err := r.applyVectorService(ctx, plat); err != nil {
		return err
	}
	return nil
}

func (r *LogicPlatformReconciler) applyVectorDaemonSet(ctx context.Context, plat *logicv1.LogicPlatform) error {
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

	return r.Apply(ctx, daemonSet, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyVectorService(ctx context.Context, plat *logicv1.LogicPlatform) error {
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

	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) reconcileVectorRBAC(ctx context.Context, plat *logicv1.LogicPlatform) error {
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
	if err := r.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
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
	if err := r.Create(ctx, rb); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (r *LogicPlatformReconciler) applyVectorConfigMap(ctx context.Context, plat *logicv1.LogicPlatform) error {
	cmName := vectorName(plat)
	childLabels := ChildLabelsInstance(plat, cmName)
	// TODO: once we add support to ES, do the check here for which config to create
	cm := corev1ac.ConfigMap(cmName, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithData(map[string]string{"vector.yaml": vectorPostgreSQLConfig})

	return r.Apply(ctx, cm, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) deleteVectorResources(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// Delete DaemonSet
	if err := r.Delete(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete Service
	if err := r.Delete(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ConfigMap
	// TODO: once we add support to ES, do the check here for which config to create
	if err := r.Delete(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ServiceAccount
	if err := r.Delete(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat), Namespace: plat.Namespace},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ClusterRoleBinding
	if err := r.Delete(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: vectorName(plat)},
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

func (r *LogicPlatformReconciler) updateVectorStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
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
	err := r.Get(ctx, client.ObjectKey{Name: vectorName(plat), Namespace: plat.Namespace}, &ds)
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

// checkPersistenceConnectivity checks database connectivity based on DataIndex pod readiness.
// If pod is ready: readiness probe already verified DB, return success.
// If pod is not ready: perform direct PostgreSQL check to diagnose startup issues.
// If deployment doesn't exist yet: skip checks (will try again on next reconcile).
func (r *LogicPlatformReconciler) checkPersistenceConnectivity(ctx context.Context, plat *logicv1.LogicPlatform) (bool, string, string) {
	log := logf.FromContext(ctx)

	// Check DataIndex deployment status
	var deployment appsv1.Deployment
	deploymentKey := client.ObjectKey{
		Name:      dataIndexName(plat),
		Namespace: plat.Namespace,
	}
	err := r.Get(ctx, deploymentKey, &deployment)
	if err != nil {
		// Deployment not created yet (normal during initial reconcile) - just skip checks
		log.V(1).Info("DataIndex deployment not found yet, persistence config is valid")
		return true, logicv1.ReasonReady, "Persistence configuration is valid"
	}

	// If pod is ready, readiness probe already verified DB connectivity
	if deployment.Status.ReadyReplicas > 0 {
		log.V(1).Info("DataIndex pod is ready, database connectivity verified")
		return true, logicv1.ReasonReady, "DataIndex healthy, database connection verified"
	}

	// Pod not ready yet - perform direct PostgreSQL check to diagnose what's blocking startup
	log.V(1).Info("DataIndex pod not ready, performing direct database check")
	return r.checkPostgresDatabaseHealth(ctx, plat)
}

// checkPostgresDatabaseHealth attempts a direct PostgreSQL connection with 3-second timeout.
// Used when DataIndex pod is not ready to diagnose startup issues.
func (r *LogicPlatformReconciler) checkPostgresDatabaseHealth(ctx context.Context, plat *logicv1.LogicPlatform) (bool, string, string) {
	log := logf.FromContext(ctx)

	pg := plat.Spec.DataIndex.Persistence.PostgreSQL
	if pg == nil {
		return false, logicv1.ReasonDatabaseUnreachable, "PostgreSQL not configured"
	}

	// Read credentials from secret
	var secret corev1.Secret
	secretKey := client.ObjectKey{
		Name:      pg.SecretRef.Name,
		Namespace: plat.Namespace,
	}
	if err := r.Get(ctx, secretKey, &secret); err != nil {
		log.V(1).Info("failed to read PostgreSQL secret", "error", err)
		return false, logicv1.ReasonDatabaseUnreachable, fmt.Sprintf("failed to read PostgreSQL credentials: %v", err)
	}

	userKey := pg.SecretRef.UserKey
	if userKey == "" {
		userKey = logicv1.DefaultPgsqlSecretUserKey
	}
	passwordKey := pg.SecretRef.PasswordKey
	if passwordKey == "" {
		passwordKey = logicv1.DefaultPgsqlSecretPasswordKey
	}

	user := string(secret.Data[userKey])
	password := string(secret.Data[passwordKey])

	// Build connection parameters
	namespace := pg.ServiceRef.Namespace
	if namespace == "" {
		namespace = plat.Namespace
	}
	port := defaultPostgresPort
	if pg.ServiceRef.Port != nil {
		port = *pg.ServiceRef.Port
	}

	dbHost := BuildPostgresAddress(pg.ServiceRef.Name, namespace, 0)

	// Test the connection via injected connector
	if r.DatabaseConnector == nil {
		log.V(1).Info("no database connector available, skipping check")
		return true, logicv1.ReasonReady, "Persistence configuration is valid"
	}

	ok, err := r.DatabaseConnector.Ping(ctx, dbHost, port, user, password, pg.ServiceRef.DatabaseName)
	if err != nil {
		log.V(1).Info("failed to connect to PostgreSQL", "error", err)
		return false, logicv1.ReasonDatabaseUnreachable, fmt.Sprintf("failed to connect to PostgreSQL: %v", err)
	}

	if ok {
		log.V(1).Info("PostgreSQL connection successful")
		return true, logicv1.ReasonReady, "PostgreSQL database connection verified"
	}

	return false, logicv1.ReasonDatabaseUnreachable, "PostgreSQL connection check failed"
}

func (r *LogicPlatformReconciler) reconcileWorkflowGateway(ctx context.Context, plat *logicv1.LogicPlatform) error {
	log := logf.FromContext(ctx)

	// Handle deletion: clean up resources before finalizer removal
	if plat.DeletionTimestamp != nil {
		if hasFinalizerWorkflowGateway(plat) {
			log.V(1).Info("LogicPlatform is being deleted, cleaning up gateway resources")
			return r.deleteWorkflowGatewayResources(ctx, plat)
		}
		return nil
	}

	// If gateway is disabled, clean up resources
	if plat.Spec.WorkflowGateway == nil {
		log.V(1).Info("WorkflowGateway is disabled, cleaning up resources")
		return r.deleteWorkflowGatewayResources(ctx, plat)
	}

	// Gateway enabled; add finalizer if not present
	if !hasFinalizerWorkflowGateway(plat) {
		original := plat.DeepCopy()
		plat.Finalizers = append(plat.Finalizers, FinalizerWorkflowGateway)
		if err := r.Patch(ctx, plat, client.MergeFrom(original)); err != nil {
			return err
		}
	}

	// Reconcile gateway resources in order
	if err := r.applyGatewayServiceAccount(ctx, plat); err != nil {
		return err
	}
	if err := r.applyGatewayClusterRoleBinding(ctx, plat); err != nil {
		return err
	}
	if err := r.applyGatewayDeployment(ctx, plat); err != nil {
		return err
	}
	if err := r.applyGatewayService(ctx, plat); err != nil {
		return err
	}
	if err := r.applyGatewayIngress(ctx, plat); err != nil {
		return err
	}

	log.V(1).Info("Gateway reconciliation complete", "platform", plat.Name)
	return nil
}

func (r *LogicPlatformReconciler) applyGatewayServiceAccount(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	saKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}

	// Check if existing resource is owned by this platform
	var existing corev1.ServiceAccount
	if err := r.Get(ctx, saKey, &existing); err == nil && !hasOwner(&existing, plat) {
		return fmt.Errorf("ServiceAccount %s already exists and is not owned by this LogicPlatform", saKey)
	}

	sa := serviceAccountForGateway(plat)
	return r.Apply(ctx, sa, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyGatewayClusterRoleBinding(ctx context.Context, plat *logicv1.LogicPlatform) error {
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
	if err := r.Get(ctx, bindingKey, &existing); err == nil {
		// If owned by us and role reference drifted (immutable field), delete and recreate
		if hasOwner(&existing, plat) && existing.RoleRef != desired.RoleRef {
			if err := r.Delete(ctx, &existing); err != nil {
				return fmt.Errorf("failed to delete drifted ClusterRoleBinding: %w", err)
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Use Patch+Apply for ClusterRoleBinding
	//nolint:staticcheck
	return r.Patch(ctx, desired, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyGatewayDeployment(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)

	deployKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	var existing appsv1.Deployment
	if err := r.Get(ctx, deployKey, &existing); err == nil && !hasOwner(&existing, plat) {
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

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyGatewayService(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)
	childLabels := ChildLabelsInstance(plat, gwName)

	svcKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	var existing corev1.Service
	if err := r.Get(ctx, svcKey, &existing); err == nil && !hasOwner(&existing, plat) {
		return fmt.Errorf("service %s already exists and is not owned by this LogicPlatform", svcKey)
	}

	spec := serviceForGateway(plat).Spec

	service := corev1ac.Service(gwName, plat.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.LogicPlatformKind)).
		WithSpec(spec)

	return r.Apply(ctx, service, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyGatewayIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	// If ingress disabled, delete any existing resources
	if plat.Spec.WorkflowGateway.Ingress == nil || !plat.Spec.WorkflowGateway.Ingress.Enabled {
		return r.deleteGatewayIngress(ctx, plat)
	}

	// Apply Ingress or Route based on platform
	if utils.IsOpenShift() {
		return r.applyGatewayRoute(ctx, plat)
	}
	return r.applyGatewayKubernetesIngress(ctx, plat)
}

func (r *LogicPlatformReconciler) deleteGatewayIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	gwName := workflowGatewayName(plat)

	// Delete Ingress if owned by this platform
	ingKey := client.ObjectKey{Name: gwName, Namespace: plat.Namespace}
	var ing networkingv1.Ingress
	if err := r.Get(ctx, ingKey, &ing); err == nil {
		if hasOwner(&ing, plat) {
			if err := r.Delete(ctx, &ing); err != nil && !apierrors.IsNotFound(err) {
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
		if err := r.Get(ctx, rtKey, &rt); err == nil {
			if hasOwner(&rt, plat) {
				if err := r.Delete(ctx, &rt); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *LogicPlatformReconciler) applyGatewayKubernetesIngress(ctx context.Context, plat *logicv1.LogicPlatform) error {
	ingress := ingressForGateway(plat)
	return r.Apply(ctx, ingress, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) applyGatewayRoute(ctx context.Context, plat *logicv1.LogicPlatform) error {
	route := routeForGateway(plat)
	//nolint:staticcheck
	return r.Patch(ctx, route, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicPlatformReconciler) deleteWorkflowGatewayResources(ctx context.Context, plat *logicv1.LogicPlatform) error {
	log := logf.FromContext(ctx)
	gwName := workflowGatewayName(plat)
	bindingName := workflowGatewayClusterRoleBindingName(plat)

	// Delete Ingress/Route
	if err := r.deleteGatewayIngress(ctx, plat); err != nil {
		return err
	}

	// Delete Service (check ownership first)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: plat.Namespace},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(svc), svc); err == nil {
		// Service exists; verify it's owned by this platform
		if !hasOwner(svc, plat) {
			return fmt.Errorf("cannot delete Service %s/%s: not owned by LogicPlatform %s/%s", svc.Namespace, svc.Name, plat.Namespace, plat.Name)
		}
		if err := r.Delete(ctx, svc); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete Deployment (check ownership first)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: plat.Namespace},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(deploy), deploy); err == nil {
		if !hasOwner(deploy, plat) {
			return fmt.Errorf("cannot delete Deployment %s/%s: not owned by LogicPlatform %s/%s", deploy.Namespace, deploy.Name, plat.Namespace, plat.Name)
		}
		if err := r.Delete(ctx, deploy); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ClusterRoleBinding (check labels first)
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: bindingName},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(binding), binding); err == nil {
		// Verify binding is labeled with this platform
		if binding.Labels["logic.kubesmarts.org/platform-namespace"] != plat.Namespace ||
			binding.Labels["logic.kubesmarts.org/platform-name"] != plat.Name {
			return fmt.Errorf("cannot delete ClusterRoleBinding %s: not labeled for LogicPlatform %s/%s", bindingName, plat.Namespace, plat.Name)
		}
		if err := r.Delete(ctx, binding); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Delete ServiceAccount (check ownership first)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, Namespace: plat.Namespace},
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(sa), sa); err == nil {
		if !hasOwner(sa, plat) {
			return fmt.Errorf("cannot delete ServiceAccount %s/%s: not owned by LogicPlatform %s/%s", sa.Namespace, sa.Name, plat.Namespace, plat.Name)
		}
		if err := r.Delete(ctx, sa); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Remove finalizer only if it was present
	if hasFinalizerWorkflowGateway(plat) {
		original := plat.DeepCopy()
		removeFinalizerWorkflowGateway(plat)
		if err := r.Patch(ctx, plat, client.MergeFrom(original)); err != nil {
			return err
		}
	}

	log.V(1).Info("Gateway resources cleaned up", "platform", plat.Name)
	return nil
}

func (r *LogicPlatformReconciler) updateWorkflowGatewayStatus(ctx context.Context, plat *logicv1.LogicPlatform) error {
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
	if err := r.Get(ctx, deployKey, &deploy); err == nil {
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
	if err := r.Get(ctx, svcKey, &svc); err == nil {
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
		url, err := ResolveIngressURL(ctx, r.Client, ingressKey, plat.Spec.WorkflowGateway.Ingress.TLS.Enabled)
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

// hasOwner checks if the given object is owned by the LogicPlatform.
func hasOwner(obj metav1.Object, plat *logicv1.LogicPlatform) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == plat.UID {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogicPlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicPlatform{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.ConfigMap{}).
		Named("logicplatform")

	// Add ownership for platform-specific resources
	if utils.IsOpenShift() {
		builder.Owns(&routev1.Route{})
	} else {
		builder.Owns(&networkingv1.Ingress{})
	}

	// Watch ClusterRoleBindings and map back to owning LogicPlatform by labels
	builder.Watches(
		&rbacv1.ClusterRoleBinding{},
		handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
			binding := obj.(*rbacv1.ClusterRoleBinding)
			ns, hasNs := binding.Labels["logic.kubesmarts.org/platform-namespace"]
			name, hasName := binding.Labels["logic.kubesmarts.org/platform-name"]
			if hasNs && hasName {
				return []ctrl.Request{
					{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}},
				}
			}
			return nil
		}),
	)

	return builder.Complete(r)
}
