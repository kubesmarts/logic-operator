package controller

import (
	"context"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	batchv1ac "k8s.io/client-go/applyconfigurations/batch/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// dbMigratorComponent implements PlatformComponent for the DBMigrator Job.
// nolint:unused // Kept intact for future re-enablement in the reconciliation loop.
type dbMigratorComponent struct {
	*ClusterLogicPlatformReconciler
}

// nolint:unused // Kept intact for future re-enablement in the reconciliation loop.
func (c *dbMigratorComponent) Name() string { return "DBMigrator" }

// nolint:unused // Kept intact for future re-enablement in the reconciliation loop.
func (c *dbMigratorComponent) Reconcile(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	log := logf.FromContext(ctx)

	// Guard: DBMigrator image not configured, skip reconciliation
	if plat.Spec.DBMigrator == nil || plat.Spec.DBMigrator.Image == "" {
		log.V(1).Info("DBMigrator image not configured, skipping reconciliation")
		return nil
	}

	// Collect configured datastores
	datastores := collectConfiguredDatastores(plat)
	if len(datastores) == 0 {
		log.V(1).Info("No databases configured, skipping DBMigrator reconciliation")
		return nil
	}

	job := buildDBMigratorJob(plat)
	if job == nil {
		return nil
	}

	return c.Apply(ctx, job, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

// nolint:unused // Kept intact for future re-enablement in the reconciliation loop.
func (c *dbMigratorComponent) UpdateStatus(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	log := logf.FromContext(ctx)

	// Guard: DBMigrator image not configured, skip status update
	if plat.Spec.DBMigrator == nil || plat.Spec.DBMigrator.Image == "" {
		return nil
	}

	// Check if any databases are configured
	datastores := collectConfiguredDatastores(plat)
	if len(datastores) == 0 {
		return nil
	}

	jobName := fmt.Sprintf("%s-db-migrator", plat.Name)
	var job batchv1.Job
	err := c.Get(ctx, client.ObjectKey{Name: jobName, Namespace: plat.Spec.TargetNamespace}, &job)

	if apierrors.IsNotFound(err) {
		// Job not yet created
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration,
			metav1.ConditionFalse, plat.Generation,
			"JobNotCreated", "DBMigrator job not yet created")
		return nil
	}
	if err != nil {
		return err
	}

	// Check job status
	for _, cond := range job.Status.Conditions {
		switch cond.Type {
		case batchv1.JobComplete:
			if cond.Status == corev1.ConditionTrue {
				logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration,
					metav1.ConditionTrue, plat.Generation,
					"Complete", "Database migration completed successfully")
				return nil
			}
		case batchv1.JobFailed:
			if cond.Status == corev1.ConditionTrue {
				logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration,
					metav1.ConditionFalse, plat.Generation,
					"Failed", fmt.Sprintf("Database migration failed: %s", cond.Message))
				return nil
			}
		}
	}

	// Job is still running
	log.V(1).Info("DBMigrator job is running", "job", jobName)
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration,
		metav1.ConditionFalse, plat.Generation,
		"InProgress", "Database migration in progress")
	return nil
}

// datastoreTarget represents a unique migration target (host:port:database:schema)
type datastoreTarget struct {
	typeName   string // logical type: dataindex, runtime, quartz
	host       string
	port       int
	database   string
	schema     string
	secretName string
	secretRef  logicv1.PostgreSQLSecretOptions // includes UserKey/PasswordKey
}

// collectConfiguredDatastores returns all unique database targets across DataIndex, Runtime, and Quartz.
// Dedup: only collapse targets that are the SAME logical type AND same connection.
func collectConfiguredDatastores(plat *logicv1.ClusterLogicPlatform) []datastoreTarget {
	var targets []datastoreTarget
	seen := make(map[string]bool) // Key: typeName:host:port:database:schema

	// DataIndex persistence
	if plat.Spec.DataIndex != nil && plat.Spec.DataIndex.Persistence != nil &&
		plat.Spec.DataIndex.Persistence.PostgreSQL != nil {
		if target := extractDatastoreTarget("dataindex", plat.Spec.DataIndex.Persistence.PostgreSQL); target != nil {
			key := fmt.Sprintf("%s:%s:%d:%s:%s", target.typeName, target.host, target.port, target.database, target.schema)
			if !seen[key] {
				targets = append(targets, *target)
				seen[key] = true
			}
		}
	}

	// Runtime datastore
	if plat.Spec.Runtime != nil && plat.Spec.Runtime.Runtime.PostgreSQL != nil {
		if target := extractDatastoreTargetSpec("runtime", &plat.Spec.Runtime.Runtime); target != nil {
			key := fmt.Sprintf("%s:%s:%d:%s:%s", target.typeName, target.host, target.port, target.database, target.schema)
			if !seen[key] {
				targets = append(targets, *target)
				seen[key] = true
			}
		}
	}

	// Quartz datastore
	if plat.Spec.Runtime != nil && plat.Spec.Runtime.Quartz.PostgreSQL != nil {
		if target := extractDatastoreTargetSpec("quartz", &plat.Spec.Runtime.Quartz); target != nil {
			key := fmt.Sprintf("%s:%s:%d:%s:%s", target.typeName, target.host, target.port, target.database, target.schema)
			if !seen[key] {
				targets = append(targets, *target)
				seen[key] = true
			}
		}
	}

	return targets
}

// extractDatastoreTarget extracts a datastoreTarget from a PersistencePostgreSQL for DataIndex.
// For DataIndex, use the operational secret (SecretRef).
func extractDatastoreTarget(name string, pg *logicv1.PersistencePostgreSQL) *datastoreTarget {
	if pg == nil {
		return nil
	}

	svc := &pg.ServiceRef
	host := BuildPostgresAddress(svc.Name, svc.Namespace, 0)
	port := defaultPostgresPort
	if svc.Port != nil {
		port = *svc.Port
	}
	database := svc.DatabaseName
	if database == "" {
		database = "postgres"
	}
	schema := pg.ServiceRef.DatabaseSchema
	if schema == "" {
		schema = "public"
	}

	return &datastoreTarget{
		typeName:   name,
		host:       host,
		port:       port,
		database:   database,
		schema:     schema,
		secretName: pg.SecretRef.Name,
		secretRef:  pg.SecretRef,
	}
}

// extractDatastoreTargetSpec extracts a datastoreTarget from a DatastoreSpec (Runtime/Quartz).
// For these, use the DDL secret (MigrationSecretRef, falling back to operational).
func extractDatastoreTargetSpec(name string, ds *logicv1.DatastoreSpec) *datastoreTarget {
	if ds == nil || ds.PostgreSQL == nil {
		return nil
	}

	svc := &ds.PostgreSQL.ServiceRef
	host := BuildPostgresAddress(svc.Name, svc.Namespace, 0)
	port := defaultPostgresPort
	if svc.Port != nil {
		port = *svc.Port
	}
	database := svc.DatabaseName
	if database == "" {
		database = "postgres"
	}
	schema := ds.PostgreSQL.ServiceRef.DatabaseSchema
	if schema == "" {
		schema = "public"
	}

	// Use DDL secret (migration secret), falling back to operational
	migSecret := logicv1.MigrationSecret(*ds)

	return &datastoreTarget{
		typeName:   name,
		host:       host,
		port:       port,
		database:   database,
		schema:     schema,
		secretName: migSecret.Name,
		secretRef:  migSecret,
	}
}

// buildDBMigratorJob constructs the Job apply configuration for database migrations.
func buildDBMigratorJob(plat *logicv1.ClusterLogicPlatform) *batchv1ac.JobApplyConfiguration {
	targets := collectConfiguredDatastores(plat)
	if len(targets) == 0 {
		return nil
	}

	jobName := fmt.Sprintf("%s-db-migrator", plat.Name)
	childLabels := ChildLabels(plat)

	// Determine image
	image := logicv1.DefaultDBMigratorImage()
	if plat.Spec.DBMigrator != nil && plat.Spec.DBMigrator.Image != "" {
		image = plat.Spec.DBMigrator.Image
	}

	// Build container spec with environment variables for all targets
	containers := corev1ac.Container().
		WithName("db-migrator").
		WithImage(image).
		WithImagePullPolicy(corev1.PullIfNotPresent)

	// Build env vars for each target
	for i, target := range targets {
		prefix := fmt.Sprintf("DB_%d_", i)

		// Determine secret keys (honor custom UserKey/PasswordKey)
		userKey := target.secretRef.UserKey
		if userKey == "" {
			userKey = logicv1.DefaultPgsqlSecretUserKey
		}
		passwordKey := target.secretRef.PasswordKey
		if passwordKey == "" {
			passwordKey = logicv1.DefaultPgsqlSecretPasswordKey
		}

		// TODO: review these env vars, should follow Quarkus Flow documentation. The documentation should be linked here and in our docs.
		// Host/Port/Database/Schema/Type
		containers.WithEnv(
			corev1ac.EnvVar().WithName(prefix+"TYPE").WithValue(target.typeName),
			corev1ac.EnvVar().WithName(prefix+"HOST").WithValue(target.host),
			corev1ac.EnvVar().WithName(prefix+"PORT").WithValue(fmt.Sprintf("%d", target.port)),
			corev1ac.EnvVar().WithName(prefix+"DATABASE").WithValue(target.database),
			corev1ac.EnvVar().WithName(prefix+"SCHEMA").WithValue(target.schema),
		)

		// Username and password from secret (using custom or default keys)
		containers.WithEnv(
			corev1ac.EnvVar().WithName(prefix+"USER").WithValueFrom(
				corev1ac.EnvVarSource().WithSecretKeyRef(
					corev1ac.SecretKeySelector().
						WithName(target.secretName).
						WithKey(userKey),
				),
			),
			corev1ac.EnvVar().WithName(prefix+"PASSWORD").WithValueFrom(
				corev1ac.EnvVarSource().WithSecretKeyRef(
					corev1ac.SecretKeySelector().
						WithName(target.secretName).
						WithKey(passwordKey),
				),
			),
		)
	}

	podSpec := corev1ac.PodSpec().
		WithContainers(containers).
		WithRestartPolicy(corev1.RestartPolicyOnFailure).
		WithServiceAccountName("")

	job := batchv1ac.Job(jobName, plat.Spec.TargetNamespace).
		WithOwnerReferences(OwnerRef(plat, logicv1.ClusterLogicPlatformKind)).
		WithLabels(childLabels).
		WithSpec(
			batchv1ac.JobSpec().
				WithTemplate(
					corev1ac.PodTemplateSpec().
						WithLabels(childLabels).
						WithSpec(podSpec),
				).
				WithBackoffLimit(int32(3)),
		)

	return job
}
