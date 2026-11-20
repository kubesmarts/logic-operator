package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MockDatabaseConnector implements DatabaseConnector for testing
type MockDatabaseConnector struct {
	ShouldSucceed bool
	LastCall      struct {
		Host   string
		Port   int
		User   string
		Dbname string
	}
}

func (m *MockDatabaseConnector) Ping(_ context.Context, host string, port int, user string, _ string, dbname string) (bool, error) {
	m.LastCall.Host = host
	m.LastCall.Port = port
	m.LastCall.User = user
	m.LastCall.Dbname = dbname
	return m.ShouldSucceed, nil
}

func newCLPReconciler() *ClusterLogicPlatformReconciler {
	return &ClusterLogicPlatformReconciler{
		Client:            k8sClient,
		Scheme:            k8sClient.Scheme(),
		DatabaseConnector: &MockDatabaseConnector{ShouldSucceed: true},
	}
}

func createCLP(ctx context.Context, name string, spec logicv1.ClusterLogicPlatformSpec) types.NamespacedName {
	nn := types.NamespacedName{Name: name}
	clp := &logicv1.ClusterLogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       spec,
	}
	Expect(k8sClient.Create(ctx, clp)).To(Succeed())
	return nn
}

func reconcileCLPAndFetch(ctx context.Context, r *ClusterLogicPlatformReconciler, nn types.NamespacedName) *logicv1.ClusterLogicPlatform {
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
	Expect(err).NotTo(HaveOccurred())
	var clp logicv1.ClusterLogicPlatform
	Expect(k8sClient.Get(ctx, nn, &clp)).To(Succeed())
	return &clp
}

var _ = Describe("ClusterLogicPlatform Controller", func() {
	Context("When targeting a specific namespace", func() {
		const name = "test-clp-target-ns"
		var nn types.NamespacedName
		var r *ClusterLogicPlatformReconciler

		BeforeEach(func() {
			r = newCLPReconciler()
			spec := logicv1.ClusterLogicPlatformSpec{
				TargetNamespace: testNamespace,
				DataIndex: &logicv1.DataIndexSpec{
					Application: logicv1.ApplicationSpec{
						Image:    logicv1.DefaultDataIndexImage(),
						Replicas: ptr.To(int32(1)),
					},
					Persistence: &logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "data-index-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{
								Name:           "data-index-postgres",
								Namespace:      testNamespace,
								Port:           ptr.To(5433),
								DatabaseName:   "data-index-db",
								DatabaseSchema: "data_index",
							},
						},
					},
				},
				Runtime: &logicv1.RuntimePlatformSpec{
					Persistence: &logicv1.RuntimePersistenceSpec{
						Runtime: logicv1.DatastoreSpec{
							PostgreSQL: &logicv1.PersistencePostgreSQL{
								SecretRef: logicv1.PostgreSQLSecretOptions{Name: "postgres-secret"},
								ServiceRef: logicv1.PostgreSQLServiceOptions{
									Name:           "postgres",
									DatabaseName:   "workflows",
									DatabaseSchema: "runtime",
								},
							},
						},
					},
				},
			}
			nn = createCLP(ctx, name, spec)
		})

		AfterEach(func() {
			clp := &logicv1.ClusterLogicPlatform{}
			if err := k8sClient.Get(ctx, nn, clp); err == nil {
				Expect(k8sClient.Delete(ctx, clp)).To(Succeed())
			}
		})

		It("should deploy Data Index Deployment into target namespace", func() {
			reconcileCLPAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			depName := types.NamespacedName{Name: name + "-data-index", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, depName, &dep)).To(Succeed())
			Expect(dep.Namespace).To(Equal(testNamespace))
		})

		It("should use Data Index persistence instead of the runtime datastore", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "data-index-secret", Namespace: testNamespace},
				Data: map[string][]byte{
					logicv1.DefaultPgsqlSecretUserKey:     []byte("data-index-user"),
					logicv1.DefaultPgsqlSecretPasswordKey: []byte("data-index-password"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			})

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "data-index-postgres", Namespace: testNamespace},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Port: 5433}},
				},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, service)).To(Succeed())
			})

			clp := reconcileCLPAndFetch(ctx, r, nn)

			var deployment appsv1.Deployment
			deploymentName := types.NamespacedName{Name: name + "-data-index", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, deploymentName, &deployment)).To(Succeed())
			container := deployment.Spec.Template.Spec.Containers[0]

			jdbcURL := findEnvVar(container.Env, "QUARKUS_DATASOURCE_JDBC_URL")
			Expect(jdbcURL).NotTo(BeNil())
			Expect(jdbcURL.Value).To(Equal("jdbc:postgresql://data-index-postgres." + testNamespace + ".svc.cluster.local:5433/data-index-db?currentSchema=data_index"))
			username := findEnvVar(container.Env, "QUARKUS_DATASOURCE_USERNAME")
			Expect(username).NotTo(BeNil())
			Expect(username.ValueFrom.SecretKeyRef.Name).To(Equal("data-index-secret"))

			connector := r.DatabaseConnector.(*MockDatabaseConnector)
			Expect(connector.LastCall.Host).To(Equal("data-index-postgres." + testNamespace + ".svc.cluster.local"))
			Expect(connector.LastCall.Port).To(Equal(5433))
			Expect(connector.LastCall.User).To(Equal("data-index-user"))
			Expect(connector.LastCall.Dbname).To(Equal("data-index-db"))
			Expect(clp.Status.DataIndex.Persistence.DatabaseConnected).NotTo(BeNil())
			Expect(*clp.Status.DataIndex.Persistence.DatabaseConnected).To(BeTrue())
		})

		It("should mark required Data Index persistence as not ready when omitted", func() {
			var clp logicv1.ClusterLogicPlatform
			Expect(k8sClient.Get(ctx, nn, &clp)).To(Succeed())
			clp.Spec.DataIndex.Persistence = nil
			Expect(k8sClient.Update(ctx, &clp)).To(Succeed())

			clp = *reconcileCLPAndFetch(ctx, r, nn)
			condition := logicv1.FindCondition(clp.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(logicv1.ReasonPersistenceConfigInvalid))
			Expect(condition.Message).To(Equal("Data Index persistence is required"))
			Expect(clp.Status.DataIndex.Persistence).NotTo(BeNil())
			Expect(clp.Status.DataIndex.Persistence.Valid).To(BeFalse())
			Expect(clp.Status.DataIndex.Persistence.Error).To(Equal(condition.Message))
		})

		It("should report missing Data Index persistence resources accurately", func() {
			const secretName = "missing-resource-secret"
			const serviceName = "missing-resource-postgres"

			var clp logicv1.ClusterLogicPlatform
			Expect(k8sClient.Get(ctx, nn, &clp)).To(Succeed())
			clp.Spec.DataIndex.Persistence.PostgreSQL.SecretRef.Name = secretName
			clp.Spec.DataIndex.Persistence.PostgreSQL.ServiceRef.Name = serviceName
			Expect(k8sClient.Update(ctx, &clp)).To(Succeed())

			clp = *reconcileCLPAndFetch(ctx, r, nn)
			Expect(clp.Status.DataIndex.Persistence.SecretExists).To(BeFalse())
			Expect(clp.Status.DataIndex.Persistence.ServiceExists).To(BeFalse())
			Expect(clp.Status.DataIndex.Persistence.Valid).To(BeFalse())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: testNamespace},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, secret)).To(Succeed()) })

			clp = *reconcileCLPAndFetch(ctx, r, nn)
			Expect(clp.Status.DataIndex.Persistence.SecretExists).To(BeTrue())
			Expect(clp.Status.DataIndex.Persistence.ServiceExists).To(BeFalse())
			Expect(clp.Status.DataIndex.Persistence.Valid).To(BeFalse())

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: testNamespace},
				Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 5432}}},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, service)).To(Succeed()) })

			clp = *reconcileCLPAndFetch(ctx, r, nn)
			Expect(clp.Status.DataIndex.Persistence.SecretExists).To(BeTrue())
			Expect(clp.Status.DataIndex.Persistence.ServiceExists).To(BeTrue())
			Expect(clp.Status.DataIndex.Persistence.Valid).To(BeTrue())
		})
	})

	Context("When targetNamespace does not exist", func() {
		const name = "test-clp-nonexistent-ns"
		var nn types.NamespacedName
		var r *ClusterLogicPlatformReconciler

		BeforeEach(func() {
			r = newCLPReconciler()
			spec := logicv1.ClusterLogicPlatformSpec{
				TargetNamespace: "nonexistent-namespace",
				DataIndex: &logicv1.DataIndexSpec{
					Application: logicv1.ApplicationSpec{
						Image:    logicv1.DefaultDataIndexImage(),
						Replicas: ptr.To(int32(1)),
					},
				},
			}
			nn = createCLP(ctx, name, spec)
		})

		AfterEach(func() {
			clp := &logicv1.ClusterLogicPlatform{}
			if err := k8sClient.Get(ctx, nn, clp); err == nil {
				Expect(k8sClient.Delete(ctx, clp)).To(Succeed())
			}
		})

		It("should set TargetNamespaceResolved to False", func() {
			clp := reconcileCLPAndFetch(ctx, r, nn)

			cond := logicv1.FindCondition(clp.Status.Conditions, logicv1.ConditionTargetNamespaceResolved)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(logicv1.ReasonTargetNamespaceMissing))
		})
	})

	Context("With Data Index dbMigrationStrategy=job", func() {
		const name = "test-clp-di-migration"
		var nn types.NamespacedName
		var r *ClusterLogicPlatformReconciler
		depName := types.NamespacedName{Name: name + "-data-index", Namespace: testNamespace}

		BeforeEach(func() {
			r = newCLPReconciler()
			nn = createCLP(ctx, name, logicv1.ClusterLogicPlatformSpec{
				TargetNamespace: testNamespace,
				DataIndex: &logicv1.DataIndexSpec{
					Persistence: &logicv1.DatastoreSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef:  logicv1.PostgreSQLSecretOptions{Name: "data-index-secret"},
							ServiceRef: logicv1.PostgreSQLServiceOptions{Name: "data-index-postgres", DatabaseSchema: "data_index"},
						},
						DBMigrationStrategy: logicv1.DBMigrationStrategyJob,
					},
				},
			})
		})

		AfterEach(func() {
			var jobs batchv1.JobList
			Expect(k8sClient.List(ctx, &jobs, client.InNamespace(testNamespace), client.MatchingLabels{LabelKeyName: name})).To(Succeed())
			for i := range jobs.Items {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &jobs.Items[i],
					client.PropagationPolicy(metav1.DeletePropagationBackground)))).To(Succeed())
			}
			var pods corev1.PodList
			Expect(k8sClient.List(ctx, &pods, client.InNamespace(testNamespace), client.HasLabels{"job-name"})).To(Succeed())
			for i := range pods.Items {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &pods.Items[i]))).To(Succeed())
			}
			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: depName.Name, Namespace: depName.Namespace}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dep))).To(Succeed())
			clp := &logicv1.ClusterLogicPlatform{}
			if err := k8sClient.Get(ctx, nn, clp); err == nil {
				Expect(k8sClient.Delete(ctx, clp)).To(Succeed())
			}
		})

		It("should deploy Data Index only after its migration Job succeeds", func() {
			clp := reconcileCLPAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(errors.IsNotFound(k8sClient.Get(ctx, depName, &dep))).To(BeTrue())
			Expect(clp.Status.DBMigrator).NotTo(BeNil())
			migration := clp.Status.DBMigrator.Migrations[0]
			Expect(migration.Datastore).To(Equal(MigrationStreamDataIndex))

			var job batchv1.Job
			jobNN := types.NamespacedName{Name: migration.JobRef, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, jobNN, &job)).To(Succeed())
			now := metav1.Now()
			job.Status.StartTime = &now
			job.Status.CompletionTime = &now
			job.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())

			// First reconcile records the success; the next one rolls out Data Index.
			clp = reconcileCLPAndFetch(ctx, r, nn)
			Expect(clp.Status.DBMigrator.Migrations[0].Phase).To(Equal(logicv1.MigrationPhaseSucceeded))
			Expect(meta.IsStatusConditionTrue(clp.Status.Conditions, logicv1.ConditionDBMigration)).To(BeTrue())
			reconcileCLPAndFetch(ctx, r, nn)

			Expect(k8sClient.Get(ctx, depName, &dep)).To(Succeed())
			envs := dep.Spec.Template.Spec.Containers[0].Env
			flyway := findEnvVar(envs, "QUARKUS_FLYWAY_MIGRATE_AT_START")
			Expect(flyway).NotTo(BeNil())
			Expect(flyway.Value).To(Equal("false"))
			Expect(findEnvVar(envs, "QUARKUS_FLYWAY_BASELINE_ON_MIGRATE")).To(BeNil())
		})
	})
})
