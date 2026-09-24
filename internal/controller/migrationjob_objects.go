package controller

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

const (
	MigrationContainerName = "db-migrator"
	MigrationIncludeEnvVar = "LOGIC_DB_MIGRATOR_INCLUDE"

	MigrationStreamRuntime   = "runtime"
	MigrationStreamQuartz    = "quartz"
	MigrationStreamDataIndex = "data-index"

	// LabelMigrationDatastore marks a migration Job with the datastore it migrates.
	LabelMigrationDatastore = "logic.kubesmarts.org/migration-datastore"

	migrationBackoffLimit            = int32(2)
	migrationActiveDeadlineSeconds   = int64(300)
	migrationTTLSecondsAfterFinished = int32(3600)

	// maxJobNameLength keeps the Job name usable as the "job-name" Pod label value.
	maxJobNameLength = 63
)

// migrationTarget is one datastore migrated by its own Job, since the migrator takes a single datasource.
type migrationTarget struct {
	Datastore string
	Spec      *logicv1.DatastoreSpec
	Include   []string
}

// migrationTargets returns every platform datastore that opted into the job strategy.
func migrationTargets(plat *logicv1.ClusterLogicPlatform) []migrationTarget {
	return append(runtimeMigrationTargets(plat), dataIndexMigrationTargets(plat)...)
}

// dataIndexMigrationTargets returns the Data Index datastore when it opted into the job strategy.
func dataIndexMigrationTargets(plat *logicv1.ClusterLogicPlatform) []migrationTarget {
	di := plat.Spec.DataIndex
	if di == nil || di.Persistence == nil || di.Persistence.PostgreSQL == nil ||
		di.Persistence.DBMigrationStrategy != logicv1.DBMigrationStrategyJob {
		return nil
	}
	return []migrationTarget{{Datastore: MigrationStreamDataIndex, Spec: di.Persistence, Include: []string{MigrationStreamDataIndex}}}
}

// runtimeMigrationTargets returns the runtime datastores that opted into the job strategy.
// Without a separate Quartz datastore the runner keeps Quartz tables on the runtime datastore,
// so the runtime Job also applies the quartz stream.
func runtimeMigrationTargets(plat *logicv1.ClusterLogicPlatform) []migrationTarget {
	if plat.Spec.Runtime == nil || plat.Spec.Runtime.Persistence == nil {
		return nil
	}
	p := plat.Spec.Runtime.Persistence
	separateQuartz := p.Quartz.PostgreSQL != nil

	var targets []migrationTarget
	if p.Runtime.PostgreSQL != nil && p.Runtime.DBMigrationStrategy == logicv1.DBMigrationStrategyJob {
		include := []string{MigrationStreamRuntime}
		if !separateQuartz {
			include = append(include, MigrationStreamQuartz)
		}
		targets = append(targets, migrationTarget{Datastore: MigrationStreamRuntime, Spec: &p.Runtime, Include: include})
	}
	if separateQuartz && p.Quartz.DBMigrationStrategy == logicv1.DBMigrationStrategyJob {
		targets = append(targets, migrationTarget{Datastore: MigrationStreamQuartz, Spec: &p.Quartz, Include: []string{MigrationStreamQuartz}})
	}
	return targets
}

// migrationEnvVars builds the datasource env vars for the migration container using the DDL credential.
func migrationEnvVars(ds *logicv1.DatastoreSpec) []*corev1ac.EnvVarApplyConfiguration {
	if ds == nil || ds.PostgreSQL == nil {
		return nil
	}
	ddl := *ds
	pg := *ds.PostgreSQL
	pg.SecretRef = logicv1.MigrationSecret(*ds)
	ddl.PostgreSQL = &pg
	return datasourcePersistenceEnvVars(&ddl, "QUARKUS_DATASOURCE_")
}

// migrationJobName returns "{platform}-db-migrator-{datastore}-{hash}". Jobs are immutable, so the
// name hashes the whole pod template: any change in behavior spawns a new Job. Secret values never
// reach the template, only their name/key coordinates, so rotating a secret does not rerun the Job.
func migrationJobName(platformName, datastore string, template *corev1.PodTemplateSpec) string {
	h := fnv.New32a()
	data, _ := json.Marshal(template)
	_, _ = h.Write(data)

	suffix := fmt.Sprintf("-%08x", h.Sum32())
	base := platformName + "-db-migrator-" + datastore
	if limit := maxJobNameLength - len(suffix); len(base) > limit {
		base = strings.TrimSuffix(base[:limit], "-")
	}
	return base + suffix
}

func buildMigrationJob(plat *logicv1.ClusterLogicPlatform, target migrationTarget) *batchv1.Job {
	var app logicv1.ApplicationSpec
	if plat.Spec.DBMigrator != nil {
		app = plat.Spec.DBMigrator.Application
	}

	labels := ChildLabels(plat)
	labels[LabelMigrationDatastore] = target.Datastore

	containerOpts := []ContainerOption{
		WithDefaultImage(logicv1.DefaultDBMigratorImage()),
		WithEnvVars(migrationEnvVars(target.Spec)...),
		WithEnvVars(envLiteral(MigrationIncludeEnvVar, strings.Join(target.Include, ","))),
		func(c *corev1ac.ContainerApplyConfiguration) {
			c.WithTerminationMessagePolicy(corev1.TerminationMessageReadFile)
		},
	}
	// Retries are Job-level (new Pod), so a container restart never overwrites the termination message.
	podOpts := []PodSpecOption{func(ps *corev1ac.PodSpecApplyConfiguration) {
		ps.WithRestartPolicy(corev1.RestartPolicyNever)
	}}
	template := convertTo[corev1.PodTemplateSpec](
		toPodTemplateSpecACWithPodOpts(MigrationContainerName, &app, labels, containerOpts, podOpts...))

	backoff, deadline, ttl := migrationBackoffLimit, migrationActiveDeadlineSeconds, migrationTTLSecondsAfterFinished
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            migrationJobName(plat.Name, target.Datastore, template),
			Namespace:       plat.Spec.TargetNamespace,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{OwnerRefStandard(plat, logicv1.ClusterLogicPlatformKind)},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: &ttl,
			Template:                *template,
		},
	}
}
