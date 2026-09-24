package controller

import (
	"context"
	"testing"

	"github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newDBMigratorComponent(t *testing.T) (*dbMigratorComponent, client.Client) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}
	if err := logicv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &dbMigratorComponent{&ClusterLogicPlatformReconciler{Client: c, Scheme: scheme}}, c
}

func listMigrationJobs(t *testing.T, c client.Client) []batchv1.Job {
	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs, client.InNamespace(testMigrationNamespace)); err != nil {
		t.Fatalf("List jobs failed: %v", err)
	}
	return jobs.Items
}

// finishMigrationJob marks the Job terminal and adds the Pod carrying the migrator's termination message.
func finishMigrationJob(t *testing.T, c client.Client, job *batchv1.Job, succeeded bool, terminationMessage string) {
	ctx := context.Background()
	completed := metav1.Now()
	condition := batchv1.JobFailed
	if succeeded {
		condition = batchv1.JobComplete
		job.Status.CompletionTime = &completed
	}
	job.Status.Conditions = []batchv1.JobCondition{{Type: condition, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(ctx, job); err != nil {
		t.Fatalf("Update job status failed: %v", err)
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: job.Name + "-pod", Namespace: job.Namespace, Labels: map[string]string{"job-name": job.Name}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  MigrationContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: terminationMessage}},
		}}},
	}
	status := pod.Status
	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("Create pod failed: %v", err)
	}
	pod.Status = status
	if err := c.Status().Update(ctx, pod); err != nil {
		t.Fatalf("Update pod status failed: %v", err)
	}
}

func TestDBMigrator_NoJobStrategyIsNoop(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	comp, c := newDBMigratorComponent(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyService), logicv1.DatastoreSpec{})
	logicv1.SetCondition(&plat.Status.Conditions, logicv1.ConditionDBMigration, metav1.ConditionTrue, 1, logicv1.ReasonReady, "")

	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())

	g.Expect(listMigrationJobs(t, c)).To(gomega.BeEmpty())
	g.Expect(plat.Status.DBMigrator).To(gomega.BeNil())
	g.Expect(meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDBMigration)).To(gomega.BeNil())
}

func TestDBMigrator_SuccessfulMigration(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	comp, c := newDBMigratorComponent(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})

	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	jobs := listMigrationJobs(t, c)
	g.Expect(jobs).To(gomega.HaveLen(1))

	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())
	cond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDBMigration)
	g.Expect(cond.Status).To(gomega.Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).To(gomega.Equal(logicv1.ReasonMigrationJobRunning))
	g.Expect(plat.Status.DBMigrator.Migrations[0].Phase).To(gomega.Equal(logicv1.MigrationPhaseRunning))

	finishMigrationJob(t, c, &jobs[0], true,
		`{"streams":[{"name":"runtime","appliedVersion":"1.0.0","appliedCount":1},{"name":"quartz","appliedVersion":"1.0.0","appliedCount":1}]}`)
	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())

	m := plat.Status.DBMigrator.Migrations[0]
	g.Expect(m.Datastore).To(gomega.Equal(MigrationStreamRuntime))
	g.Expect(m.JobRef).To(gomega.Equal(jobs[0].Name))
	g.Expect(m.Phase).To(gomega.Equal(logicv1.MigrationPhaseSucceeded))
	g.Expect(m.Streams).To(gomega.HaveLen(2))
	g.Expect(m.Streams[0].AppliedVersion).To(gomega.Equal("1.0.0"))
	g.Expect(m.Streams[0].LastAppliedAt).NotTo(gomega.BeNil())

	cond = meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDBMigration)
	g.Expect(cond.Status).To(gomega.Equal(metav1.ConditionTrue))

	// Once the TTL removes the succeeded Job, it is not recreated and the status is kept.
	g.Expect(c.Delete(ctx, &jobs[0])).To(gomega.Succeed())
	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	g.Expect(listMigrationJobs(t, c)).To(gomega.BeEmpty())
	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())
	g.Expect(plat.Status.DBMigrator.Migrations[0].Phase).To(gomega.Equal(logicv1.MigrationPhaseSucceeded))
}

func TestDBMigrator_FailedMigration(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	comp, c := newDBMigratorComponent(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})

	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	jobs := listMigrationJobs(t, c)
	finishMigrationJob(t, c, &jobs[0], false, `{"error":"connection refused"}`)
	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())

	m := plat.Status.DBMigrator.Migrations[0]
	g.Expect(m.Phase).To(gomega.Equal(logicv1.MigrationPhaseFailed))
	g.Expect(m.Message).To(gomega.Equal("connection refused"))
	cond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDBMigration)
	g.Expect(cond.Status).To(gomega.Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).To(gomega.Equal(logicv1.ReasonMigrationJobFailed))
	g.Expect(cond.Message).To(gomega.ContainSubstring("connection refused"))
	g.Expect(logicv1.DerivePhase(plat.Status.Conditions, 1)).To(gomega.Equal(logicv1.ApplicationPhaseFailed))

	// A failed Job reruns once deleted.
	g.Expect(c.Delete(ctx, &jobs[0])).To(gomega.Succeed())
	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	g.Expect(listMigrationJobs(t, c)).To(gomega.HaveLen(1))
}

func TestDBMigrator_SeparateQuartzDatastore(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	comp, c := newDBMigratorComponent(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), testDatastore("quartz", logicv1.DBMigrationStrategyJob))

	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	g.Expect(listMigrationJobs(t, c)).To(gomega.HaveLen(2))

	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())
	g.Expect(plat.Status.DBMigrator.Migrations).To(gomega.HaveLen(2))
	g.Expect(plat.Status.DBMigrator.Migrations[1].Datastore).To(gomega.Equal(MigrationStreamQuartz))
}

func TestDBMigrator_ReplacesStaleJobs(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	comp, c := newDBMigratorComponent(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})

	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	old := listMigrationJobs(t, c)[0].Name

	plat.Spec.DBMigrator = &logicv1.DBMigratorSpec{Application: logicv1.ApplicationSpec{Image: "other:1"}}
	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	jobs := listMigrationJobs(t, c)
	g.Expect(jobs).To(gomega.HaveLen(1))
	g.Expect(jobs[0].Name).ToNot(gomega.Equal(old))

	plat.Spec.Runtime.Persistence.Runtime.DBMigrationStrategy = logicv1.DBMigrationStrategyService
	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	g.Expect(listMigrationJobs(t, c)).To(gomega.BeEmpty())
}

func TestDBMigrator_DataIndexDatastore(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()
	comp, c := newDBMigratorComponent(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})
	ds := testDatastore("data_index", logicv1.DBMigrationStrategyJob)
	ds.MigrationSecretRef = &logicv1.PostgreSQLSecretOptions{Name: "di-ddl-creds"}
	plat.Spec.DataIndex = &logicv1.DataIndexSpec{Persistence: &ds}

	g.Expect(comp.Reconcile(ctx, plat)).To(gomega.Succeed())
	jobs := listMigrationJobs(t, c)
	g.Expect(jobs).To(gomega.HaveLen(2))

	var diJob *batchv1.Job
	for i := range jobs {
		if jobs[i].Labels[LabelMigrationDatastore] == MigrationStreamDataIndex {
			diJob = &jobs[i]
		}
	}
	g.Expect(diJob).NotTo(gomega.BeNil())
	envs := diJob.Spec.Template.Spec.Containers[0].Env
	g.Expect(findCoreEnv(envs, MigrationIncludeEnvVar).Value).To(gomega.Equal(MigrationStreamDataIndex))
	g.Expect(findCoreEnv(envs, "QUARKUS_DATASOURCE_USERNAME").ValueFrom.SecretKeyRef.Name).To(gomega.Equal("di-ddl-creds"))

	finishMigrationJob(t, c, diJob, true, `{"streams":[{"name":"data-index","appliedVersion":"3","appliedCount":3}]}`)
	g.Expect(comp.UpdateStatus(ctx, plat)).To(gomega.Succeed())
	diTargets := dataIndexMigrationTargets(plat)
	g.Expect(pendingMigration(plat, diTargets)).To(gomega.BeNil())
	g.Expect(pendingMigration(plat, runtimeMigrationTargets(plat))).NotTo(gomega.BeNil())
	cond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDBMigration)
	g.Expect(cond.Reason).To(gomega.Equal(logicv1.ReasonMigrationJobRunning))
}
