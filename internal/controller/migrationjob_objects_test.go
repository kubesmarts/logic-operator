package controller

import (
	"strings"
	"testing"

	"github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testMigrationNamespace = "workflows"

func testDatastore(schema string, strategy logicv1.DBMigrationStrategyType) logicv1.DatastoreSpec {
	return logicv1.DatastoreSpec{
		PostgreSQL: &logicv1.PersistencePostgreSQL{
			SecretRef:  logicv1.PostgreSQLSecretOptions{Name: testPGCreds},
			ServiceRef: logicv1.PostgreSQLServiceOptions{Name: "postgres", DatabaseName: "logicflow", DatabaseSchema: schema},
		},
		DBMigrationStrategy: strategy,
	}
}

func testMigrationPlatform(runtimeDS, quartzDS logicv1.DatastoreSpec) *logicv1.ClusterLogicPlatform {
	return &logicv1.ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "test-platform", UID: "abc-123", Generation: 1},
		Spec: logicv1.ClusterLogicPlatformSpec{
			TargetNamespace: testMigrationNamespace,
			Runtime: &logicv1.RuntimePlatformSpec{
				Persistence: &logicv1.RuntimePersistenceSpec{Runtime: runtimeDS, Quartz: quartzDS},
			},
		},
	}
}

func findCoreEnv(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range envs {
		if envs[i].Name == name {
			return &envs[i]
		}
	}
	return nil
}

func TestMigrationTargets(t *testing.T) {
	job, service := logicv1.DBMigrationStrategyJob, logicv1.DBMigrationStrategyService

	t.Run("no runtime persistence", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := &logicv1.ClusterLogicPlatform{}
		g.Expect(migrationTargets(plat)).To(gomega.BeEmpty())
	})

	t.Run("service strategy", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(testDatastore("runtime", service), logicv1.DatastoreSpec{})
		g.Expect(migrationTargets(plat)).To(gomega.BeEmpty())
	})

	t.Run("runtime job without quartz migrates only the runtime stream", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(testDatastore("runtime", job), logicv1.DatastoreSpec{})
		targets := migrationTargets(plat)
		g.Expect(targets).To(gomega.HaveLen(1))
		g.Expect(targets[0].Datastore).To(gomega.Equal(MigrationStreamRuntime))
		g.Expect(targets[0].Include).To(gomega.Equal([]string{MigrationStreamRuntime}))
	})

	t.Run("quartz without postgresql inherits the runtime connection", func(t *testing.T) {
		g := gomega.NewWithT(t)
		runtimeDS := testDatastore("runtime", job)
		runtimeDS.MigrationSecretRef = &logicv1.PostgreSQLSecretOptions{Name: "ddl-creds"}
		plat := testMigrationPlatform(runtimeDS, logicv1.DatastoreSpec{DBMigrationStrategy: job})
		targets := migrationTargets(plat)
		g.Expect(targets).To(gomega.HaveLen(2))
		g.Expect(targets[1].Datastore).To(gomega.Equal(MigrationStreamQuartz))
		g.Expect(targets[1].Include).To(gomega.Equal([]string{MigrationStreamQuartz}))
		g.Expect(targets[1].Spec.PostgreSQL.ServiceRef.DatabaseSchema).To(gomega.Equal("runtime"))
		g.Expect(targets[1].Spec.MigrationSecretRef.Name).To(gomega.Equal("ddl-creds"))
		// The platform spec is not mutated by the inheritance.
		g.Expect(plat.Spec.Runtime.Persistence.Quartz.PostgreSQL).To(gomega.BeNil())
	})

	t.Run("quartz with its own postgresql keeps its own DDL credential", func(t *testing.T) {
		g := gomega.NewWithT(t)
		runtimeDS := testDatastore("runtime", job)
		runtimeDS.MigrationSecretRef = &logicv1.PostgreSQLSecretOptions{Name: "ddl-creds"}
		plat := testMigrationPlatform(runtimeDS, testDatastore("quartz", job))
		targets := migrationTargets(plat)
		g.Expect(targets).To(gomega.HaveLen(2))
		g.Expect(targets[1].Spec.MigrationSecretRef).To(gomega.BeNil())
	})

	t.Run("quartz without postgresql and no runtime postgresql has no target", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(logicv1.DatastoreSpec{}, logicv1.DatastoreSpec{DBMigrationStrategy: job})
		g.Expect(migrationTargets(plat)).To(gomega.BeEmpty())
	})

	t.Run("separate quartz datastore gets its own target", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(testDatastore("runtime", job), testDatastore("quartz", job))
		targets := migrationTargets(plat)
		g.Expect(targets).To(gomega.HaveLen(2))
		g.Expect(targets[0].Include).To(gomega.Equal([]string{MigrationStreamRuntime}))
		g.Expect(targets[1].Datastore).To(gomega.Equal(MigrationStreamQuartz))
		g.Expect(targets[1].Include).To(gomega.Equal([]string{MigrationStreamQuartz}))
		g.Expect(targets[1].Spec.PostgreSQL.ServiceRef.DatabaseSchema).To(gomega.Equal("quartz"))
	})

	t.Run("data index job gets its own target", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(testDatastore("runtime", service), logicv1.DatastoreSpec{})
		ds := testDatastore("data_index", job)
		plat.Spec.DataIndex = &logicv1.DataIndexSpec{Persistence: &ds}
		g.Expect(flowRunnerMigrationTargets(plat)).To(gomega.BeEmpty())
		targets := migrationTargets(plat)
		g.Expect(targets).To(gomega.HaveLen(1))
		g.Expect(targets[0].Datastore).To(gomega.Equal(MigrationStreamDataIndex))
		g.Expect(targets[0].Include).To(gomega.Equal([]string{MigrationStreamDataIndex}))
		g.Expect(targets[0].Spec.PostgreSQL.ServiceRef.DatabaseSchema).To(gomega.Equal("data_index"))
	})

	t.Run("data index service strategy", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(logicv1.DatastoreSpec{}, logicv1.DatastoreSpec{})
		ds := testDatastore("data_index", service)
		plat.Spec.DataIndex = &logicv1.DataIndexSpec{Persistence: &ds}
		g.Expect(migrationTargets(plat)).To(gomega.BeEmpty())
	})

	t.Run("only separate quartz opts in", func(t *testing.T) {
		g := gomega.NewWithT(t)
		plat := testMigrationPlatform(testDatastore("runtime", service), testDatastore("quartz", job))
		targets := migrationTargets(plat)
		g.Expect(targets).To(gomega.HaveLen(1))
		g.Expect(targets[0].Datastore).To(gomega.Equal(MigrationStreamQuartz))
	})
}

func TestMigrationEnvVars_UsesDDLSecret(t *testing.T) {
	g := gomega.NewWithT(t)
	ds := testDatastore("runtime", logicv1.DBMigrationStrategyJob)
	ds.MigrationSecretRef = &logicv1.PostgreSQLSecretOptions{Name: "ddl-creds", UserKey: "user"}

	plat := testMigrationPlatform(ds, logicv1.DatastoreSpec{})
	job := buildMigrationJob(plat, migrationTargets(plat)[0])
	envs := job.Spec.Template.Spec.Containers[0].Env

	user := findCoreEnv(envs, "QUARKUS_DATASOURCE_USERNAME")
	g.Expect(user).NotTo(gomega.BeNil())
	g.Expect(user.ValueFrom.SecretKeyRef.Name).To(gomega.Equal("ddl-creds"))
	g.Expect(user.ValueFrom.SecretKeyRef.Key).To(gomega.Equal("user"))
	g.Expect(findCoreEnv(envs, "QUARKUS_DATASOURCE_JDBC_URL").Value).
		To(gomega.Equal("jdbc:postgresql://postgres:5432/logicflow?currentSchema=runtime"))
	g.Expect(findCoreEnv(envs, "QUARKUS_HIBERNATE_ORM_SCHEMA_MANAGEMENT_STRATEGY")).To(gomega.BeNil())
	// The operational secret is left untouched for the runtime.
	g.Expect(ds.PostgreSQL.SecretRef.Name).To(gomega.Equal(testPGCreds))
}

func TestBuildMigrationJob_Defaults(t *testing.T) {
	g := gomega.NewWithT(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})

	job := buildMigrationJob(plat, migrationTargets(plat)[0])

	g.Expect(job.Name).To(gomega.HavePrefix("test-platform-db-migrator-runtime-"))
	g.Expect(job.Namespace).To(gomega.Equal(testMigrationNamespace))
	g.Expect(job.Labels).To(gomega.HaveKeyWithValue(LabelMigrationDatastore, MigrationStreamRuntime))
	g.Expect(job.OwnerReferences).To(gomega.HaveLen(1))
	g.Expect(job.OwnerReferences[0].Kind).To(gomega.Equal(logicv1.ClusterLogicPlatformKind))
	g.Expect(job.OwnerReferences[0].Name).To(gomega.Equal(plat.Name))

	g.Expect(*job.Spec.BackoffLimit).To(gomega.Equal(int32(2)))
	g.Expect(*job.Spec.ActiveDeadlineSeconds).To(gomega.Equal(int64(300)))
	g.Expect(*job.Spec.TTLSecondsAfterFinished).To(gomega.Equal(int32(3600)))

	pod := job.Spec.Template.Spec
	g.Expect(pod.RestartPolicy).To(gomega.Equal(corev1.RestartPolicyNever))
	g.Expect(*pod.SecurityContext.RunAsNonRoot).To(gomega.BeTrue())
	g.Expect(pod.Containers).To(gomega.HaveLen(1))

	c := pod.Containers[0]
	g.Expect(c.Name).To(gomega.Equal(MigrationContainerName))
	g.Expect(c.Image).To(gomega.Equal(logicv1.DefaultDBMigratorImage()))
	g.Expect(c.TerminationMessagePolicy).To(gomega.Equal(corev1.TerminationMessageReadFile))
	g.Expect(*c.SecurityContext.AllowPrivilegeEscalation).To(gomega.BeFalse())
	g.Expect(findCoreEnv(c.Env, MigrationIncludeEnvVar).Value).To(gomega.Equal("runtime"))
}

func TestBuildMigrationJob_HonorsApplicationSpec(t *testing.T) {
	g := gomega.NewWithT(t)
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})
	plat.Spec.DBMigrator = &logicv1.DBMigratorSpec{Application: logicv1.ApplicationSpec{
		Image: "my-registry.io/my-migrator:v1.2.3",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		},
		PodTemplate: logicv1.PodTemplateSpec{PodSpec: logicv1.PodSpec{RestartPolicy: corev1.RestartPolicyAlways}},
	}}

	job := buildMigrationJob(plat, migrationTargets(plat)[0])
	c := job.Spec.Template.Spec.Containers[0]
	g.Expect(c.Image).To(gomega.Equal("my-registry.io/my-migrator:v1.2.3"))
	g.Expect(c.Resources.Limits.Memory().String()).To(gomega.Equal("256Mi"))
	g.Expect(job.Spec.Template.Spec.RestartPolicy).To(gomega.Equal(corev1.RestartPolicyNever))
}

func TestMigrationJobName(t *testing.T) {
	plat := testMigrationPlatform(testDatastore("runtime", logicv1.DBMigrationStrategyJob), logicv1.DatastoreSpec{})
	nameOf := func(p *logicv1.ClusterLogicPlatform) string {
		return buildMigrationJob(p, migrationTargets(p)[0]).Name
	}
	base := nameOf(plat)

	t.Run("stable for identical inputs", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(nameOf(plat.DeepCopy())).To(gomega.Equal(base))
	})

	t.Run("changes with image", func(t *testing.T) {
		g := gomega.NewWithT(t)
		changed := plat.DeepCopy()
		changed.Spec.DBMigrator = &logicv1.DBMigratorSpec{Application: logicv1.ApplicationSpec{Image: "other:1"}}
		g.Expect(nameOf(changed)).ToNot(gomega.Equal(base))
	})

	t.Run("changes with connection", func(t *testing.T) {
		g := gomega.NewWithT(t)
		changed := plat.DeepCopy()
		changed.Spec.Runtime.Persistence.Runtime.PostgreSQL.ServiceRef.DatabaseName = "other"
		g.Expect(nameOf(changed)).ToNot(gomega.Equal(base))
	})

	t.Run("changes with secret coordinates", func(t *testing.T) {
		g := gomega.NewWithT(t)
		changed := plat.DeepCopy()
		changed.Spec.Runtime.Persistence.Runtime.MigrationSecretRef = &logicv1.PostgreSQLSecretOptions{Name: "ddl-creds"}
		g.Expect(nameOf(changed)).ToNot(gomega.Equal(base))
	})

	t.Run("unaffected by quartz configuration", func(t *testing.T) {
		g := gomega.NewWithT(t)
		changed := plat.DeepCopy()
		changed.Spec.Runtime.Persistence.Quartz = logicv1.DatastoreSpec{DBMigrationStrategy: logicv1.DBMigrationStrategyJob}
		g.Expect(nameOf(changed)).To(gomega.Equal(base))
	})

	t.Run("truncates long platform names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		changed := plat.DeepCopy()
		changed.Name = strings.Repeat("a", 80)
		name := nameOf(changed)
		g.Expect(len(name)).To(gomega.BeNumerically("<=", maxJobNameLength))
		g.Expect(name).ToNot(gomega.ContainSubstring("--"))
	})
}
