package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/utils/kubernetes"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// dbMigratorComponent implements PlatformComponent for the schema migration Jobs.
type dbMigratorComponent struct {
	*ClusterLogicPlatformReconciler
}

func (c *dbMigratorComponent) Name() string { return "DBMigrator" }

// Reconcile creates one Job per datastore with dbMigrationStrategy=job and removes Jobs no longer desired.
func (c *dbMigratorComponent) Reconcile(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	log := logf.FromContext(ctx)

	desired := make(map[string]struct{})
	var errs []error
	for _, target := range migrationTargets(plat) {
		job := buildMigrationJob(plat, target)
		desired[job.Name] = struct{}{}

		existing, err := kubernetes.FindJob(ctx, c.Client, job.Namespace, job.Name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// A succeeded Job removed by its TTL must not rerun; a failed one reruns once deleted.
		if existing != nil || migrationSucceeded(plat, target.Datastore, job.Name) {
			continue
		}
		log.Info("creating schema migration Job", "job", job.Name, "datastore", target.Datastore)
		if err := c.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			errs = append(errs, err)
		}
	}

	errs = append(errs, c.deleteStaleMigrationJobs(ctx, plat, desired))
	return errors.Join(errs...)
}

func (c *dbMigratorComponent) deleteStaleMigrationJobs(ctx context.Context, plat *logicv1.ClusterLogicPlatform, desired map[string]struct{}) error {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs,
		client.InNamespace(plat.Spec.TargetNamespace),
		client.MatchingLabels{LabelKeyName: plat.Name, LabelKeyManagedBy: LabelManagedBy},
		client.HasLabels{LabelMigrationDatastore},
	); err != nil {
		return err
	}
	for i := range jobs.Items {
		if _, ok := desired[jobs.Items[i].Name]; ok {
			continue
		}
		if err := c.Delete(ctx, &jobs.Items[i], client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// UpdateStatus mirrors each migration Job onto plat.Status.DBMigrator and the DBMigration condition.
func (c *dbMigratorComponent) UpdateStatus(ctx context.Context, plat *logicv1.ClusterLogicPlatform) error {
	targets := migrationTargets(plat)
	if len(targets) == 0 {
		plat.Status.DBMigrator = nil
		meta.RemoveStatusCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration)
		return nil
	}

	migrations := make([]logicv1.DatastoreMigrationStatus, 0, len(targets))
	for _, target := range targets {
		m, err := c.observeMigration(ctx, plat, target)
		if err != nil {
			return err
		}
		migrations = append(migrations, m)
	}
	plat.Status.DBMigrator = &logicv1.DBMigratorStatus{Migrations: migrations}

	var running, failed []string
	for _, m := range migrations {
		switch m.Phase {
		case logicv1.MigrationPhaseFailed:
			msg := m.Datastore
			if m.Message != "" {
				msg += ": " + m.Message
			}
			failed = append(failed, msg)
		case logicv1.MigrationPhaseSucceeded:
		default:
			running = append(running, m.Datastore)
		}
	}
	switch {
	case len(failed) > 0:
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration, metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonMigrationJobFailed, "Schema migration failed for "+strings.Join(failed, "; "))
	case len(running) > 0:
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration, metav1.ConditionFalse, plat.Generation,
			logicv1.ReasonMigrationJobRunning, "Schema migration in progress for "+strings.Join(running, ", "))
	default:
		logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration, metav1.ConditionTrue, plat.Generation,
			logicv1.ReasonReady, "Schema migration completed")
	}
	return nil
}

func (c *dbMigratorComponent) observeMigration(ctx context.Context, plat *logicv1.ClusterLogicPlatform, target migrationTarget) (logicv1.DatastoreMigrationStatus, error) {
	jobName := buildMigrationJob(plat, target).Name
	status := logicv1.DatastoreMigrationStatus{Datastore: target.Datastore, JobRef: jobName, Phase: logicv1.MigrationPhasePending}

	job, err := kubernetes.FindJob(ctx, c.Client, plat.Spec.TargetNamespace, jobName)
	if err != nil {
		return status, err
	}
	if job == nil {
		if migrationSucceeded(plat, target.Datastore, jobName) {
			return *previousMigration(plat, target.Datastore), nil
		}
		return status, nil
	}

	finished, succeeded := kubernetes.JobHasFinished(job)
	if !finished {
		status.Phase = logicv1.MigrationPhaseRunning
		return status, nil
	}

	report, reportErr := c.harvestMigrationReport(ctx, job)
	status.Streams = toStreamStatus(report.Streams, job.Status.CompletionTime)
	if succeeded {
		status.Phase = logicv1.MigrationPhaseSucceeded
		return status, nil
	}
	status.Phase = logicv1.MigrationPhaseFailed
	status.Message = report.Error
	if reportErr != nil {
		status.Message = reportErr.Error()
	}
	if status.Message == "" {
		status.Message = fmt.Sprintf("Job %s failed", jobName)
	}
	return status, nil
}

// harvestMigrationReport reads the migrator's termination-message report off the Job's most
// recently created Pod. backoffLimit can produce more than one Pod (retries), so the
// list is sorted by CreationTimestamp rather than trusting List's return order.
func (c *dbMigratorComponent) harvestMigrationReport(ctx context.Context, job *batchv1.Job) (migrationReport, error) {
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name}); err != nil {
		return migrationReport{}, err
	}
	if len(pods.Items) == 0 {
		return migrationReport{}, nil
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp)
	})
	pod := pods.Items[len(pods.Items)-1]
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == MigrationContainerName && cs.State.Terminated != nil {
			return parseMigrationReport(cs.State.Terminated.Message)
		}
	}
	return migrationReport{}, nil
}

func previousMigration(plat *logicv1.ClusterLogicPlatform, datastore string) *logicv1.DatastoreMigrationStatus {
	if plat.Status.DBMigrator == nil {
		return nil
	}
	for i := range plat.Status.DBMigrator.Migrations {
		if plat.Status.DBMigrator.Migrations[i].Datastore == datastore {
			return &plat.Status.DBMigrator.Migrations[i]
		}
	}
	return nil
}

// pendingMigration returns the first target whose current Job has not succeeded according to
// plat.Status, or nil when all have. Matching the JobRef ignores results of outdated Jobs.
func pendingMigration(plat *logicv1.ClusterLogicPlatform, targets []migrationTarget) *logicv1.DatastoreMigrationStatus {
	for _, target := range targets {
		jobName := buildMigrationJob(plat, target).Name
		prev := previousMigration(plat, target.Datastore)
		if prev == nil || prev.JobRef != jobName {
			return &logicv1.DatastoreMigrationStatus{Datastore: target.Datastore, JobRef: jobName, Phase: logicv1.MigrationPhasePending}
		}
		if prev.Phase != logicv1.MigrationPhaseSucceeded {
			return prev
		}
	}
	return nil
}

func migrationSucceeded(plat *logicv1.ClusterLogicPlatform, datastore, jobName string) bool {
	prev := previousMigration(plat, datastore)
	return prev != nil && prev.JobRef == jobName && prev.Phase == logicv1.MigrationPhaseSucceeded
}

// toStreamStatus uses the Job's completion time so repeated reconciles keep the status stable.
func toStreamStatus(streams []migrationStreamReport, completedAt *metav1.Time) []logicv1.MigrationStreamStatus {
	if len(streams) == 0 {
		return nil
	}
	out := make([]logicv1.MigrationStreamStatus, 0, len(streams))
	for _, s := range streams {
		out = append(out, logicv1.MigrationStreamStatus{
			Name:           s.Name,
			AppliedVersion: s.AppliedVersion,
			AppliedCount:   s.AppliedCount,
			LastAppliedAt:  completedAt,
		})
	}
	return out
}
