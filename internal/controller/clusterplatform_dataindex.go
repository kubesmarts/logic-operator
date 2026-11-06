package controller

import (
	"context"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	routev1 "github.com/openshift/api/route/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// dataIndexComponent implements PlatformComponent for the Data Index service.
type dataIndexComponent struct {
	*ClusterLogicPlatformReconciler
}

func (c *dataIndexComponent) Name() string { return "Data Index" }
func (c *dataIndexComponent) Reconcile(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	return c.reconcileDataIndex(ctx, plat)
}
func (c *dataIndexComponent) UpdateStatus(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	return c.updateDataIndexStatus(ctx, plat)
}

func (c *dataIndexComponent) reconcileDataIndex(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	log := logf.FromContext(ctx)

	if plat.Spec.DataIndex == nil {
		log.V(1).Info("DataIndex is disabled, skipping reconciliation")
		return nil
	}

	if err := c.applyDataIndexDeployment(ctx, plat); err != nil {
		return err
	}
	if err := c.applyDataIndexService(ctx, plat); err != nil {
		return err
	}
	// Apply or delete Ingress/Route based on enabled flag
	if err := c.applyDataIndexIngress(ctx, plat); err != nil {
		return err
	}
	return nil
}

func (c *dataIndexComponent) applyDataIndexDeployment(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	childLabels := ChildLabelsInstance(plat, dataIndexNameCLP(plat))
	// Use operational secret from Runtime.Runtime datastore
	runtimePersistence := platformRuntimePersistence(plat)
	opts := []ContainerOption{
		WithDefaultImage(logicv1.DefaultDataIndexImage()),
		WithQuarkusPersistenceEnvVars(runtimePersistence),
		WithFlywayPersistenceVars(),
		WithGraphQLVars(),
		DefaultQuarkusProbes(),
	}
	spec := ToDeploymentSpec(
		ContainerNameDataIndex,
		&plat.Spec.DataIndex.Application,
		childLabels,
		SelectorLabels(dataIndexNameCLP(plat)),
		opts...,
	)
	deployment := appsv1ac.Deployment(dataIndexNameCLP(plat), plat.Spec.TargetNamespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(plat, logicv1.ClusterLogicPlatformKind)).
		WithSpec(spec)

	return c.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *dataIndexComponent) applyDataIndexService(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	diName := dataIndexNameCLP(plat)
	childLabels := ChildLabelsInstance(plat, diName)
	svc := corev1ac.Service(diName, plat.Spec.TargetNamespace).
		WithOwnerReferences(OwnerRef(plat, logicv1.ClusterLogicPlatformKind)).
		WithLabels(childLabels).
		WithSpec(
			corev1ac.ServiceSpec().
				WithSelector(SelectorLabels(diName)).
				WithPorts(
					corev1ac.ServicePort().
						WithName("http").
						WithProtocol(corev1.ProtocolTCP).
						WithPort(defaultPort).
						WithTargetPort(intstr.FromInt32(logicv1.QuarkusFlowRunnerHTTPPort)),
				),
		)
	return c.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *dataIndexComponent) applyDataIndexIngress(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	// If ingress is not enabled, delete any existing Ingress/Route resources
	if plat.Spec.DataIndex == nil || plat.Spec.DataIndex.Ingress == nil || !plat.Spec.DataIndex.Ingress.Enabled {
		return c.deleteDataIndexIngress(ctx, plat)
	}

	// Apply Ingress/Route based on platform
	if utils.IsOpenShift() {
		return c.applyDataIndexRoute(ctx, plat)
	}
	return c.applyDataIndexKubernetesIngress(ctx, plat)
}

func (c *dataIndexComponent) deleteDataIndexIngress(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	// Delete Kubernetes Ingress if it exists
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dataIndexNameCLP(plat),
			Namespace: plat.Spec.TargetNamespace,
		},
	}
	if err := c.Delete(ctx, ingress); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Delete OpenShift Route if it exists
	if utils.IsOpenShift() {
		route := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dataIndexNameCLP(plat),
				Namespace: plat.Spec.TargetNamespace,
			},
		}
		if err := c.Delete(ctx, route); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func (c *dataIndexComponent) applyDataIndexKubernetesIngress(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	desired := ingressForDataIndexCLP(plat)
	return c.Apply(ctx, desired, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *dataIndexComponent) applyDataIndexRoute(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	desired := routeForDataIndexCLP(plat)
	//nolint:staticcheck // OpenShift Route API has no apply configurations; deprecated Patch+client.Apply is the only option
	return c.Patch(ctx, desired, client.Apply, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (c *dataIndexComponent) updateDataIndexStatusDeployment(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	var deployment appsv1.Deployment
	err := c.Get(ctx, objectKeyDataIndexCLP(plat), &deployment)
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
	if plat.Spec.DataIndex != nil && plat.Spec.DataIndex.Application.Replicas != nil {
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

func (c *dataIndexComponent) updateDeploymentStatusService(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	var svc corev1.Service
	err := c.Get(ctx, objectKeyDataIndexCLP(plat), &svc)
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
	if plat.Spec.DataIndex != nil && plat.Spec.DataIndex.Ingress != nil && plat.Spec.DataIndex.Ingress.Enabled {
		externalURL, err := c.resolveExternalURL(ctx, plat)
		if err != nil {
			return err
		}
		if externalURL != "" {
			baseURL = externalURL
			plat.Status.DataIndex.Service.URL = externalURL
		} else {
			// Ingress enabled but URL not yet resolved, use internal
			baseURL = c.internalServiceURL(plat)
			plat.Status.DataIndex.Service.URL = ""
		}
	} else {
		// No ingress, use internal service URL
		baseURL = c.internalServiceURL(plat)
		plat.Status.DataIndex.Service.URL = ""
	}

	// Set endpoint URLs
	plat.Status.DataIndex.Service.GraphQLEndpoint = fmt.Sprintf("%s/graphql", baseURL)
	plat.Status.DataIndex.Service.MetricsEndpoint = fmt.Sprintf("%s/q/metrics", baseURL)

	return nil
}

func (c *dataIndexComponent) internalServiceURL(plat *logicv1.ClusterLogicPlatform) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d",
		dataIndexNameCLP(plat), plat.Spec.TargetNamespace, defaultPort)
}

func (c *dataIndexComponent) resolveExternalURL(ctx context.Context, plat *logicv1.ClusterLogicPlatform) (string, error) {
	tlsEnabled := false
	if plat.Spec.DataIndex != nil && plat.Spec.DataIndex.Ingress != nil {
		tlsEnabled = plat.Spec.DataIndex.Ingress.TLS.Enabled
	}
	return ResolveIngressURL(ctx, c.Client, objectKeyDataIndexCLP(plat), tlsEnabled)
}

func (c *dataIndexComponent) updateDataIndexStatus(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	// If disabled, clear status and conditions
	if plat.Spec.DataIndex == nil {
		plat.Status.DataIndex = nil
		removeClusterLogicPlatformCondition(&plat.Status.Conditions,
			logicv1.ConditionDataIndexReady,
			logicv1.ConditionDataIndexDeploymentAvailable,
			logicv1.ConditionDataIndexServiceReady,
			logicv1.ConditionDataIndexPersistenceReady,
		)
		return nil
	}

	// Enabled: initialize status if nil
	if plat.Status.DataIndex == nil {
		plat.Status.DataIndex = &logicv1.DataIndexStatus{}
	}

	diName := dataIndexNameCLP(plat)
	// Set resource references
	plat.Status.DataIndex.Service.DeploymentRef.Name = diName
	plat.Status.DataIndex.Service.ServiceRef.Name = diName

	// Update persistence, deployment, and service status.
	if err := c.updateStatusPersistence(ctx, plat); err != nil {
		return err
	}
	if err := c.updateDataIndexStatusDeployment(ctx, plat); err != nil {
		return err
	}
	if err := c.updateDeploymentStatusService(ctx, plat); err != nil {
		return err
	}

	ready, reason, message := clusterPlatformComponentReadiness(plat.Status.Conditions,
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

	return nil
}

func (c *dataIndexComponent) updateStatusPersistence(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	// Use operational secret from Runtime.Runtime datastore
	if plat.Spec.Runtime == nil {
		plat.Status.DataIndex.Persistence = nil
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
			metav1.ConditionTrue, plat.Generation,
			logicv1.ReasonReady, "No persistence configured")
		return nil
	}
	runtimeDS := plat.Spec.Runtime.Runtime
	if runtimeDS.PostgreSQL == nil {
		plat.Status.DataIndex.Persistence = nil
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady,
			metav1.ConditionTrue, plat.Generation,
			logicv1.ReasonReady, "No persistence configured")
		return nil
	}

	pg := runtimeDS.PostgreSQL
	operationalSecret := logicv1.OperationalSecret(runtimeDS)
	status := &logicv1.PersistenceConfigStatus{
		Valid:        true,
		SecretExists: false,
	}

	// Check if secret exists in target namespace
	var secret corev1.Secret
	secretKey := client.ObjectKey{
		Name:      operationalSecret.Name,
		Namespace: plat.Spec.TargetNamespace,
	}
	err := c.Get(ctx, secretKey, &secret)
	if apierrors.IsNotFound(err) {
		status.Valid = false
		status.SecretExists = false
		status.Error = fmt.Sprintf("secret %s not found", operationalSecret.Name)
	} else if err != nil {
		return err
	}

	status.SecretExists = true

	// Check if service exists
	var svc corev1.Service
	svcNamespace := pg.ServiceRef.Namespace
	if svcNamespace == "" {
		svcNamespace = plat.Spec.TargetNamespace
	}
	svcKey := client.ObjectKey{
		Name:      pg.ServiceRef.Name,
		Namespace: svcNamespace,
	}
	err = c.Get(ctx, svcKey, &svc)
	if apierrors.IsNotFound(err) {
		status.Valid = false
		status.ServiceExists = false
		if status.Error != "" {
			status.Error += "; "
		}
		status.Error += fmt.Sprintf("service %s not found", pg.ServiceRef.Name)
	} else if err != nil {
		return err
	}

	status.ServiceExists = true

	plat.Status.DataIndex.Persistence = status

	// Set condition based on validation
	if status.Valid {
		// Configuration is valid, check database connectivity
		healthy, reason, message := c.checkPersistenceConnectivity(ctx, plat)
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

func (c *dataIndexComponent) checkPersistenceConnectivity(ctx context.Context, plat *logicv1.ClusterLogicPlatform) (bool, string, string) {
	log := logf.FromContext(ctx)

	// Check DataIndex deployment status
	var deployment appsv1.Deployment
	deploymentKey := client.ObjectKey{
		Name:      dataIndexNameCLP(plat),
		Namespace: plat.Spec.TargetNamespace,
	}
	err := c.Get(ctx, deploymentKey, &deployment)
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
	return c.checkPostgresDatabaseHealth(ctx, plat)
}

func (c *dataIndexComponent) checkPostgresDatabaseHealth(ctx context.Context, plat *logicv1.ClusterLogicPlatform) (bool, string, string) {
	log := logf.FromContext(ctx)

	if plat.Spec.Runtime == nil {
		return false, logicv1.ReasonDatabaseUnreachable, "PostgreSQL not configured"
	}
	runtimeDS := plat.Spec.Runtime.Runtime
	pg := runtimeDS.PostgreSQL
	if pg == nil {
		return false, logicv1.ReasonDatabaseUnreachable, "PostgreSQL not configured"
	}

	operationalSecret := logicv1.OperationalSecret(runtimeDS)

	// Read credentials from secret
	var secret corev1.Secret
	secretKey := client.ObjectKey{
		Name:      operationalSecret.Name,
		Namespace: plat.Spec.TargetNamespace,
	}
	if err := c.Get(ctx, secretKey, &secret); err != nil {
		log.V(1).Info("failed to read PostgreSQL secret", "error", err)
		return false, logicv1.ReasonDatabaseUnreachable, fmt.Sprintf("failed to read PostgreSQL credentials: %v", err)
	}

	userKey := operationalSecret.UserKey
	if userKey == "" {
		userKey = logicv1.DefaultPgsqlSecretUserKey
	}
	passwordKey := operationalSecret.PasswordKey
	if passwordKey == "" {
		passwordKey = logicv1.DefaultPgsqlSecretPasswordKey
	}

	user := string(secret.Data[userKey])
	password := string(secret.Data[passwordKey])

	// Build connection parameters
	namespace := pg.ServiceRef.Namespace
	if namespace == "" {
		namespace = plat.Spec.TargetNamespace
	}
	port := defaultPostgresPort
	if pg.ServiceRef.Port != nil {
		port = *pg.ServiceRef.Port
	}

	dbHost := BuildPostgresAddress(pg.ServiceRef.Name, namespace, 0)

	// Test the connection via injected connector
	if c.DatabaseConnector == nil {
		log.V(1).Info("no database connector available, skipping check")
		return true, logicv1.ReasonReady, "Persistence configuration is valid"
	}

	ok, err := c.DatabaseConnector.Ping(ctx, dbHost, port, user, password, pg.ServiceRef.DatabaseName)
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

// platformRuntimePersistence returns a DatastoreSpec wrapping the operational secret from Runtime.Runtime
func platformRuntimePersistence(plat *logicv1.ClusterLogicPlatform) *logicv1.DatastoreSpec {
	if plat.Spec.Runtime == nil || plat.Spec.Runtime.Runtime.PostgreSQL == nil {
		return nil
	}
	return &plat.Spec.Runtime.Runtime
}

func removeClusterLogicPlatformCondition(conditions *[]metav1.Condition, conditionTypes ...string) {
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

func clusterPlatformComponentReadiness(conditions []metav1.Condition, conditionTypes ...string) (bool, string, string) {
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
