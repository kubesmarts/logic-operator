package controller

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func reconcileAndFetch(ctx context.Context, r *LogicFlowRuntimeReconciler, nn types.NamespacedName) *logicv1.LogicFlowRuntime {
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
	Expect(err).NotTo(HaveOccurred())
	var rt logicv1.LogicFlowRuntime
	Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
	return &rt
}

func findEnvVar(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range envs {
		if envs[i].Name == name {
			return &envs[i]
		}
	}
	return nil
}

func newReconciler() *LogicFlowRuntimeReconciler {
	return &LogicFlowRuntimeReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

func createRuntime(ctx context.Context, name string, spec logicv1.LogicFlowRuntimeSpec) types.NamespacedName {
	nn := types.NamespacedName{Name: name, Namespace: testNamespace}
	rt := &logicv1.LogicFlowRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       spec,
	}
	Expect(k8sClient.Create(ctx, rt)).To(Succeed())
	return nn
}

func deleteRuntime(ctx context.Context, nn types.NamespacedName) {
	rt := &logicv1.LogicFlowRuntime{}
	err := k8sClient.Get(ctx, nn, rt)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, rt)).To(Succeed())
}

func mainContainer(dep *appsv1.Deployment) corev1.Container {
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == ContainerNameRunner {
			return c
		}
	}
	Fail("container " + ContainerNameRunner + " not found in Deployment")
	return corev1.Container{}
}

func createFlowConfigMap(ctx context.Context, name, runtimeRef, workflowName, workflowVersion string) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				testLabelKeyName:            name,
				testLabelKeyManagedBy:       LabelManagedBy,
				"app.kubernetes.io/part-of": LabelPartOf,
				logicv1.LabelRuntimeRef:             runtimeRef,
				logicv1.LabelWorkflowName:           workflowName,
				logicv1.LabelWorkflowVersion:        workflowVersion,
			},
		},
		Data: map[string]string{
			workflowName + ".yaml": "document:\n  name: " + workflowName + "\n",
		},
	}
	Expect(k8sClient.Create(ctx, cm)).To(Succeed())
}

func deleteConfigMap(ctx context.Context, name string) {
	cm := &corev1.ConfigMap{}
	nn := types.NamespacedName{Name: name, Namespace: testNamespace}
	err := k8sClient.Get(ctx, nn, cm)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
}

func findVolume(dep *appsv1.Deployment, name string) *corev1.Volume {
	for i := range dep.Spec.Template.Spec.Volumes {
		if dep.Spec.Template.Spec.Volumes[i].Name == name {
			return &dep.Spec.Template.Spec.Volumes[i]
		}
	}
	return nil
}

func findVolumeMount(c corev1.Container, name string) *corev1.VolumeMount {
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == name {
			return &c.VolumeMounts[i]
		}
	}
	return nil
}

func persistenceSpec() logicv1.LogicFlowRuntimeSpec {
	return logicv1.LogicFlowRuntimeSpec{
		RuntimeSpec: logicv1.RuntimeSpec{
			Persistence: &logicv1.PersistenceOptionsSpec{
				PostgreSQL: &logicv1.PersistencePostgreSQL{
					SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
					JdbcUrl:   "jdbc:postgresql://pg.default.svc:5432/logicflow",
				},
			},
		},
	}
}

func listLeases(ctx context.Context, poolName string) []coordinationv1.Lease {
	var list coordinationv1.LeaseList
	err := k8sClient.List(ctx, &list,
		client.InNamespace(testNamespace),
		client.MatchingLabels{LabelDurablePool: poolName},
	)
	Expect(err).NotTo(HaveOccurred())
	return list.Items
}

func deleteLeases(ctx context.Context, poolName string) {
	leases := listLeases(ctx, poolName)
	for i := range leases {
		_ = k8sClient.Delete(ctx, &leases[i])
	}
}

var _ = Describe("LogicFlowRuntime Controller", func() {

	Context("Minimal runtime (no persistence, no security)", func() {
		const name = "test-minimal"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should create a Deployment with minimal image and probes", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			c := mainContainer(&dep)
			expectedImage := fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, ImageVariantMinimal)
			Expect(c.Image).To(Equal(expectedImage))

			Expect(c.LivenessProbe).NotTo(BeNil())
			Expect(c.LivenessProbe.HTTPGet.Path).To(Equal("/q/health/live"))
			Expect(c.LivenessProbe.HTTPGet.Port.IntValue()).To(Equal(int(QuarkusPort)))

			Expect(c.ReadinessProbe).NotTo(BeNil())
			Expect(c.ReadinessProbe.HTTPGet.Path).To(Equal("/q/health/ready"))
			Expect(c.ReadinessProbe.HTTPGet.Port.IntValue()).To(Equal(int(QuarkusPort)))

			Expect(dep.Labels).To(HaveKeyWithValue(testLabelKeyName, name))
			Expect(dep.Labels).To(HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))

			Expect(dep.Spec.Selector.MatchLabels).To(Equal(SelectorLabels(name)))

			secEnv := findEnvVar(c.Env, "QUARKUS_FLOW_RUNNER_SECURITY_TYPE")
			Expect(secEnv).NotTo(BeNil())
			Expect(secEnv.Value).To(Equal("none"))

			Expect(findEnvVar(c.Env, "QUARKUS_DATASOURCE_DB_KIND")).To(BeNil())
		})

		It("should create a Service with port 80 targeting 8080", func() {
			reconcileAndFetch(ctx, r, nn)

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, nn, &svc)).To(Succeed())

			Expect(svc.Spec.Ports).To(HaveLen(1))
			port := svc.Spec.Ports[0]
			Expect(port.Name).To(Equal("http"))
			Expect(port.Protocol).To(Equal(corev1.ProtocolTCP))
			Expect(port.Port).To(Equal(int32(80)))
			Expect(port.TargetPort.IntValue()).To(Equal(int(QuarkusPort)))

			Expect(svc.Spec.Selector).To(Equal(SelectorLabels(name)))
		})

		It("should set owner references on child resources", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.OwnerReferences).To(HaveLen(1))
			Expect(dep.OwnerReferences[0].APIVersion).To(Equal(logicv1.GroupVersion.String()))
			Expect(dep.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowRuntimeKind))
			Expect(dep.OwnerReferences[0].Name).To(Equal(name))
			Expect(dep.OwnerReferences[0].UID).To(Equal(rt.UID))
			Expect(*dep.OwnerReferences[0].Controller).To(BeTrue())
			Expect(*dep.OwnerReferences[0].BlockOwnerDeletion).To(BeTrue())

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, nn, &svc)).To(Succeed())
			Expect(svc.OwnerReferences).To(HaveLen(1))
			Expect(svc.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowRuntimeKind))
			Expect(svc.OwnerReferences[0].UID).To(Equal(rt.UID))
		})

		It("should set status fields on first reconcile", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.ObservedGeneration).To(Equal(rt.Generation))
			Expect(rt.Status.DeploymentRef.Name).To(Equal(name))
			Expect(rt.Status.ServiceRef.Name).To(Equal(name))
			Expect(rt.Status.Selector).To(Equal(labels.Set(SelectorLabels(name)).String()))
			Expect(rt.Status.Replicas).To(Equal(int32(0)))
			Expect(rt.Status.ReadyReplicas).To(Equal(int32(0)))

			depCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionDeploymentAvailable)
			Expect(depCond).NotTo(BeNil())
			Expect(depCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCond.Reason).To(Equal(logicv1.ReasonDeploymentProgressing))

			svcCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionServiceReady)
			Expect(svcCond).NotTo(BeNil())
			Expect(svcCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(svcCond.Reason).To(Equal(logicv1.ReasonReady))

			Expect(rt.Status.Phase).To(Equal(logicv1.ApplicationPhasePending))
		})
	})

	Context("With persistence", func() {
		const name = "test-persist"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{
				RuntimeSpec: logicv1.RuntimeSpec{
					Persistence: &logicv1.PersistenceOptionsSpec{
						PostgreSQL: &logicv1.PersistencePostgreSQL{
							SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
							JdbcUrl:   "jdbc:postgresql://pg.default.svc:5432/logicflow",
						},
					},
				},
			})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should create a Deployment with the standard runner image", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			c := mainContainer(&dep)

			expectedImage := fmt.Sprintf("%s/%s:%s-%s", QuarkusFlowRegistry, QuarkusFlowRunner, QuarkusFlowVersion, ImageVariantStandard)
			Expect(c.Image).To(Equal(expectedImage))
		})

		It("should include persistence environment variables", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			envs := mainContainer(&dep).Env

			dbKind := findEnvVar(envs, "QUARKUS_DATASOURCE_DB_KIND")
			Expect(dbKind).NotTo(BeNil())
			Expect(dbKind.Value).To(Equal("postgresql"))

			user := findEnvVar(envs, "QUARKUS_DATASOURCE_USERNAME")
			Expect(user).NotTo(BeNil())
			Expect(user.ValueFrom.SecretKeyRef.Name).To(Equal("pg-creds"))
			Expect(user.ValueFrom.SecretKeyRef.Key).To(Equal("POSTGRESQL_USER"))

			password := findEnvVar(envs, "QUARKUS_DATASOURCE_PASSWORD")
			Expect(password).NotTo(BeNil())
			Expect(password.ValueFrom.SecretKeyRef.Name).To(Equal("pg-creds"))

			jdbcUrl := findEnvVar(envs, "QUARKUS_DATASOURCE_JDBC_URL")
			Expect(jdbcUrl).NotTo(BeNil())
			Expect(jdbcUrl.Value).To(Equal("jdbc:postgresql://pg.default.svc:5432/logicflow"))
		})
	})

	Context("With API_KEY security", func() {
		const name = "test-apikey"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{
				Security: logicv1.RuntimeSecuritySpec{
					Type: logicv1.RuntimeSecurityAPIKey,
					APIKey: &logicv1.APIKeyAuthSpec{
						Keys: []logicv1.APIKeySpec{
							{
								Name:      "svc-key",
								SecretRef: logicv1.SecretKeySelector{Name: "api-secret", Key: "token"},
								Roles:     []logicv1.RuntimeSecurityRole{logicv1.RuntimeSecurityRoleAdmin},
							},
						},
					},
				},
			})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should include API key security environment variables", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			envs := mainContainer(&dep).Env

			secType := findEnvVar(envs, "QUARKUS_FLOW_RUNNER_SECURITY_TYPE")
			Expect(secType).NotTo(BeNil())
			Expect(secType.Value).To(Equal("api-key"))

			secretEnv := findEnvVar(envs, `QUARKUS_FLOW_RUNNER_SECURITY_API_KEYS__"svc-key"__SECRET`)
			Expect(secretEnv).NotTo(BeNil())
			Expect(secretEnv.ValueFrom.SecretKeyRef.Name).To(Equal("api-secret"))
			Expect(secretEnv.ValueFrom.SecretKeyRef.Key).To(Equal("token"))

			rolesEnv := findEnvVar(envs, `QUARKUS_FLOW_RUNNER_SECURITY_API_KEYS__"svc-key"__ROLES`)
			Expect(rolesEnv).NotTo(BeNil())
			Expect(rolesEnv.Value).To(Equal("flow-admin"))
		})
	})

	Context("Status derivation with patched Deployment", func() {
		const name = "test-status"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should set Phase=Ready when deployment is available with replicas", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			dep.Status.Replicas = 1
			dep.Status.ReadyReplicas = 1
			dep.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentAvailable,
					Status:  corev1.ConditionTrue,
					Reason:  "MinimumReplicasAvailable",
					Message: "Deployment has minimum availability.",
				},
			}
			Expect(k8sClient.Status().Update(ctx, &dep)).To(Succeed())

			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Replicas).To(Equal(int32(1)))
			Expect(rt.Status.ReadyReplicas).To(Equal(int32(1)))

			depCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionDeploymentAvailable)
			Expect(depCond).NotTo(BeNil())
			Expect(depCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(rt.Status.Phase).To(Equal(logicv1.ApplicationPhaseReady))
		})

		It("should set Phase=Failed on ProgressDeadlineExceeded", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			dep.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentProgressing,
					Status:  corev1.ConditionFalse,
					Reason:  logicv1.ReasonProgressDeadlineExceeded,
					Message: "timed out",
				},
			}
			Expect(k8sClient.Status().Update(ctx, &dep)).To(Succeed())

			rt := reconcileAndFetch(ctx, r, nn)

			depCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionDeploymentAvailable)
			Expect(depCond).NotTo(BeNil())
			Expect(depCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCond.Reason).To(Equal(logicv1.ReasonProgressDeadlineExceeded))

			Expect(rt.Status.Phase).To(Equal(logicv1.ApplicationPhaseFailed))
		})

		It("should propagate Replicas and ReadyReplicas from Deployment status", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			dep.Status.Replicas = 3
			dep.Status.ReadyReplicas = 2
			Expect(k8sClient.Status().Update(ctx, &dep)).To(Succeed())

			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Replicas).To(Equal(int32(3)))
			Expect(rt.Status.ReadyReplicas).To(Equal(int32(2)))
		})
	})

	Context("Idempotency", func() {
		const name = "test-idempotent"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should produce the same result on repeated reconciliation", func() {
			rt1 := reconcileAndFetch(ctx, r, nn)
			phase1 := rt1.Status.Phase
			gen1 := rt1.Status.ObservedGeneration
			condCount1 := len(rt1.Status.Conditions)

			rt2 := reconcileAndFetch(ctx, r, nn)
			Expect(rt2.Status.Phase).To(Equal(phase1))
			Expect(rt2.Status.ObservedGeneration).To(Equal(gen1))
			Expect(rt2.Status.Conditions).To(HaveLen(condCount1))
		})
	})

	Context("CR not found", func() {
		It("should return success for a missing CR", func() {
			r := newReconciler()
			nn := types.NamespacedName{Name: "does-not-exist", Namespace: testNamespace}

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var dep appsv1.Deployment
			err = k8sClient.Get(ctx, nn, &dep)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("Spec updates via SSA", func() {
		const name = "test-ssa"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should update the Deployment when spec image changes", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(mainContainer(&dep).Image).To(Equal(runnerImage(ImageVariantMinimal)))

			var rt logicv1.LogicFlowRuntime
			Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
			rt.Spec.Image = "custom-registry/my-runner:2.0"
			Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

			reconcileAndFetch(ctx, r, nn)

			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(mainContainer(&dep).Image).To(Equal("custom-registry/my-runner:2.0"))
		})

		It("should update ObservedGeneration after spec change", func() {
			rt := reconcileAndFetch(ctx, r, nn)
			gen1 := rt.Status.ObservedGeneration

			Expect(k8sClient.Get(ctx, nn, rt)).To(Succeed())
			rt.Spec.Image = "custom-registry/my-runner:3.0"
			Expect(k8sClient.Update(ctx, rt)).To(Succeed())

			rt = reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.ObservedGeneration).To(BeNumerically(">", gen1))
			Expect(rt.Status.ObservedGeneration).To(Equal(rt.Generation))
		})
	})

	Context("Source path env var", func() {
		const name = "test-source-path"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should always set QUARKUS_FLOW_RUNNER_SOURCE_PATH", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			c := mainContainer(&dep)
			env := findEnvVar(c.Env, "QUARKUS_FLOW_RUNNER_SOURCE_PATH")
			Expect(env).NotTo(BeNil())
			Expect(env.Value).To(Equal(WorkflowMountPath))
		})

		It("should have empty definitions and configMapRefs with no ConfigMaps", func() {
			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())
		})
	})

	Context("With one workflow ConfigMap", func() {
		const name = "test-one-cm"
		const cmName = "lfd-payment-processor"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cmName, name, "payment-processor", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should add a volume for the ConfigMap", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			vol := findVolume(&dep, cmName)
			Expect(vol).NotTo(BeNil(), "volume for ConfigMap not found")
			Expect(vol.ConfigMap).NotTo(BeNil())
			Expect(vol.ConfigMap.Name).To(Equal(cmName))
		})

		It("should add a volumeMount for the ConfigMap", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			c := mainContainer(&dep)

			vm := findVolumeMount(c, cmName)
			Expect(vm).NotTo(BeNil(), "volumeMount for ConfigMap not found")
			Expect(vm.MountPath).To(Equal(WorkflowMountPath + "/payment-processor.yaml"))
			Expect(vm.SubPath).To(Equal("payment-processor.yaml"))
			Expect(vm.ReadOnly).To(BeTrue())
		})

		It("should populate status.definitions with workflow metadata", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Name).To(Equal("payment-processor"))
			Expect(rt.Status.Definitions[0].Version).To(Equal("1.0.0"))
			Expect(rt.Status.Definitions[0].Service).To(BeEmpty())
		})

		It("should populate status.configMapRefs", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.ConfigMapRefs).To(HaveLen(1))
			Expect(rt.Status.ConfigMapRefs[0].Name).To(Equal(cmName))
		})
	})

	Context("With multiple workflow ConfigMaps", func() {
		const name = "test-multi-cm"
		const cm1Name = "lfd-order-flow"
		const cm2Name = "lfd-payment-processor"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cm1Name, name, "order-flow", "2.0.0")
			createFlowConfigMap(ctx, cm2Name, name, "payment-processor", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cm1Name)
			deleteConfigMap(ctx, cm2Name)
			deleteRuntime(ctx, nn)
		})

		It("should add volumes sorted by name", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			var flowVols []string
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if strings.HasPrefix(v.Name, ConfigMapPrefix) {
					flowVols = append(flowVols, v.Name)
				}
			}
			Expect(flowVols).To(Equal([]string{cm1Name, cm2Name}))
		})

		It("should populate status with all definitions sorted by ConfigMap name", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(HaveLen(2))
			Expect(rt.Status.Definitions[0].Name).To(Equal("order-flow"))
			Expect(rt.Status.Definitions[1].Name).To(Equal("payment-processor"))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(2))
			Expect(rt.Status.ConfigMapRefs[0].Name).To(Equal(cm1Name))
			Expect(rt.Status.ConfigMapRefs[1].Name).To(Equal(cm2Name))
		})
	})

	Context("ConfigMap lifecycle", func() {
		const name = "test-cm-lifecycle"
		const cmName = "lfd-lifecycle-wf"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should add volume when ConfigMap appears after initial reconcile", func() {
			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Definitions).To(BeEmpty())

			createFlowConfigMap(ctx, cmName, name, "lifecycle-wf", "1.0.0")
			rt = reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(1))

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).NotTo(BeNil())
		})

		It("should remove volume when ConfigMap is deleted", func() {
			createFlowConfigMap(ctx, cmName, name, "lifecycle-wf", "1.0.0")
			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Definitions).To(HaveLen(1))

			deleteConfigMap(ctx, cmName)
			rt = reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).To(BeNil())
		})
	})

	Context("ConfigMap with wrong runtime-ref", func() {
		const name = "test-wrong-ref"
		const cmName = "lfd-wrong-ref-wf"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cmName, "other-runtime", "wrong-ref-wf", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should not include ConfigMap in volumes or status", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).To(BeNil())
		})
	})

	Context("Idempotency with ConfigMaps", func() {
		const name = "test-idem-cm"
		const cmName = "lfd-idem-wf"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cmName, name, "idem-wf", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should produce the same result on repeated reconciliation with ConfigMaps", func() {
			rt1 := reconcileAndFetch(ctx, r, nn)
			Expect(rt1.Status.Definitions).To(HaveLen(1))

			rt2 := reconcileAndFetch(ctx, r, nn)
			Expect(rt2.Status.Definitions).To(HaveLen(1))
			Expect(rt2.Status.Definitions[0].Name).To(Equal(rt1.Status.Definitions[0].Name))
			Expect(rt2.Status.ConfigMapRefs).To(HaveLen(1))
			Expect(rt2.Status.ConfigMapRefs[0].Name).To(Equal(rt1.Status.ConfigMapRefs[0].Name))
		})
	})

	Context("Lease creation with persistence", func() {
		const name = "test-lease"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, persistenceSpec())
		})
		AfterEach(func() {
			deleteLeases(ctx, name)

			deleteRuntime(ctx, nn)
		})

		It("should create one lease for default replicas", func() {
			reconcileAndFetch(ctx, r, nn)

			leases := listLeases(ctx, name)
			Expect(leases).To(HaveLen(1))
			Expect(leases[0].Name).To(Equal(fmt.Sprintf(LeaseMemberNameFmt, name, 0)))
			Expect(*leases[0].Spec.LeaseDurationSeconds).To(Equal(LeaseDuration))
			Expect(leases[0].Spec.HolderIdentity).To(BeNil())
		})

		It("should set correct labels on leases", func() {
			reconcileAndFetch(ctx, r, nn)

			leases := listLeases(ctx, name)
			Expect(leases[0].Labels).To(HaveKeyWithValue(testLabelKeyManagedBy, DurableManagedByValue))
			Expect(leases[0].Labels).To(HaveKeyWithValue("app.kubernetes.io/component", DurableComponentValue))
			Expect(leases[0].Labels).To(HaveKeyWithValue(LabelDurablePool, name))
			Expect(leases[0].Labels).To(HaveKeyWithValue(LabelDurableIsLeader, "false"))
		})

		It("should set Deployment owner reference on leases", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			leases := listLeases(ctx, name)
			Expect(leases[0].OwnerReferences).To(HaveLen(1))
			Expect(leases[0].OwnerReferences[0].Kind).To(Equal("Deployment"))
			Expect(leases[0].OwnerReferences[0].Name).To(Equal(name))
			Expect(leases[0].OwnerReferences[0].UID).To(Equal(dep.UID))
			Expect(*leases[0].OwnerReferences[0].Controller).To(BeFalse())
		})

		It("should set LeaseReady condition and LeaseReplicas", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.LeaseReplicas).To(Equal(int32(1)))

			leaseCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionLeaseReady)
			Expect(leaseCond).NotTo(BeNil())
			Expect(leaseCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("No leases without persistence", func() {
		const name = "test-no-lease"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should not create any leases", func() {
			reconcileAndFetch(ctx, r, nn)

			leases := listLeases(ctx, name)
			Expect(leases).To(BeEmpty())
		})

		It("should not set LeaseReady condition", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			leaseCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionLeaseReady)
			Expect(leaseCond).To(BeNil())
		})
	})

	Context("Lease scaling", func() {
		const name = "test-lease-scale"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, persistenceSpec())
		})
		AfterEach(func() {
			deleteLeases(ctx, name)

			deleteRuntime(ctx, nn)
		})

		It("should create additional leases on scale up", func() {
			reconcileAndFetch(ctx, r, nn)
			Expect(listLeases(ctx, name)).To(HaveLen(1))

			var rt logicv1.LogicFlowRuntime
			Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
			replicas := int32(3)
			rt.Spec.Replicas = &replicas
			Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

			rt2 := reconcileAndFetch(ctx, r, nn)
			leases := listLeases(ctx, name)
			Expect(leases).To(HaveLen(3))
			Expect(rt2.Status.LeaseReplicas).To(Equal(int32(3)))
		})

		It("should delete excess leases on scale down", func() {
			var rt logicv1.LogicFlowRuntime
			Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
			replicas := int32(3)
			rt.Spec.Replicas = &replicas
			Expect(k8sClient.Update(ctx, &rt)).To(Succeed())
			reconcileAndFetch(ctx, r, nn)
			Expect(listLeases(ctx, name)).To(HaveLen(3))

			Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
			replicas = int32(1)
			rt.Spec.Replicas = &replicas
			Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

			rt2 := reconcileAndFetch(ctx, r, nn)
			leases := listLeases(ctx, name)
			Expect(leases).To(HaveLen(1))
			Expect(leases[0].Name).To(Equal(fmt.Sprintf(LeaseMemberNameFmt, name, 0)))
			Expect(rt2.Status.LeaseReplicas).To(Equal(int32(1)))
		})
	})

	Context("Lease idempotency", func() {
		const name = "test-lease-idem"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, persistenceSpec())
		})
		AfterEach(func() {
			deleteLeases(ctx, name)

			deleteRuntime(ctx, nn)
		})

		It("should produce the same leases on repeated reconciliation", func() {
			reconcileAndFetch(ctx, r, nn)
			leases1 := listLeases(ctx, name)
			Expect(leases1).To(HaveLen(1))

			reconcileAndFetch(ctx, r, nn)
			leases2 := listLeases(ctx, name)
			Expect(leases2).To(HaveLen(1))
			Expect(leases2[0].Name).To(Equal(leases1[0].Name))
			Expect(leases2[0].UID).To(Equal(leases1[0].UID))
		})
	})

	Context("Durable env vars on Deployment", func() {
		const name = "test-durable-env"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, persistenceSpec())
		})
		AfterEach(func() {
			deleteLeases(ctx, name)

			deleteRuntime(ctx, nn)
		})

		It("should set durable env vars when persistence is configured", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			c := mainContainer(&dep)

			leader := findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED")
			Expect(leader).NotTo(BeNil())
			Expect(leader.Value).To(Equal("false"))

			pool := findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME")
			Expect(pool).NotTo(BeNil())
			Expect(pool.Value).To(Equal(name))

			podName := findEnvVar(c.Env, "POD_NAME")
			Expect(podName).NotTo(BeNil())
			Expect(podName.ValueFrom.FieldRef.FieldPath).To(Equal("metadata.name"))

			podNs := findEnvVar(c.Env, "POD_NAMESPACE")
			Expect(podNs).NotTo(BeNil())
			Expect(podNs.ValueFrom.FieldRef.FieldPath).To(Equal("metadata.namespace"))
		})

		It("should not set durable env vars without persistence", func() {
			deleteLeases(ctx, name)

			deleteRuntime(ctx, nn)

			nn = createRuntime(ctx, "test-no-durable-env", logicv1.LogicFlowRuntimeSpec{})
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-no-durable-env", Namespace: testNamespace}, &dep)).To(Succeed())
			c := mainContainer(&dep)

			Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED")).To(BeNil())
			Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME")).To(BeNil())

			deleteRuntime(ctx, types.NamespacedName{Name: "test-no-durable-env", Namespace: testNamespace})
		})
	})

	Context("Deployment strategy with persistence", func() {
		const name = "test-strategy"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, persistenceSpec())
		})
		AfterEach(func() {
			deleteLeases(ctx, name)
			deleteRuntime(ctx, nn)
		})

		It("should use Recreate strategy for single replica", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		})

		It("should use RollingUpdate with maxUnavailable=1 for multiple replicas", func() {
			var rt logicv1.LogicFlowRuntime
			Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
			replicas := int32(3)
			rt.Spec.Replicas = &replicas
			Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
			Expect(dep.Spec.Strategy.RollingUpdate).NotTo(BeNil())
			Expect(dep.Spec.Strategy.RollingUpdate.MaxUnavailable.IntValue()).To(Equal(1))
			Expect(dep.Spec.Strategy.RollingUpdate.MaxSurge.IntValue()).To(Equal(1))
		})
	})

	Context("Deployment strategy without persistence", func() {
		const name = "test-no-strategy"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should not override the default strategy", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
		})
	})

	Context("Pod RBAC with persistence", func() {
		const name = "test-rbac"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, persistenceSpec())
		})
		AfterEach(func() {
			deleteLeases(ctx, name)

			deleteRuntime(ctx, nn)
		})

		It("should create a ServiceAccount", func() {
			reconcileAndFetch(ctx, r, nn)

			var sa corev1.ServiceAccount
			Expect(k8sClient.Get(ctx, nn, &sa)).To(Succeed())
			Expect(sa.OwnerReferences).To(HaveLen(1))
			Expect(sa.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowRuntimeKind))
		})

		It("should create a RoleBinding", func() {
			reconcileAndFetch(ctx, r, nn)

			var rb rbacv1.RoleBinding
			rbNN := types.NamespacedName{Name: name + "-durable", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, rbNN, &rb)).To(Succeed())
			Expect(rb.RoleRef.Name).To(Equal(ClusterRoleDurable))
			Expect(rb.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Name).To(Equal(name))
			Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(rb.OwnerReferences).To(HaveLen(1))
			Expect(rb.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowRuntimeKind))
		})

		It("should set serviceAccountName on the Deployment", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(name))
		})
	})

	Context("No RBAC without persistence", func() {
		const name = "test-no-rbac"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should not create ServiceAccount or RoleBinding", func() {
			reconcileAndFetch(ctx, r, nn)

			var sa corev1.ServiceAccount
			err := k8sClient.Get(ctx, nn, &sa)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			var rb rbacv1.RoleBinding
			rbNN := types.NamespacedName{Name: name + "-durable", Namespace: testNamespace}
			err = k8sClient.Get(ctx, rbNN, &rb)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should not set serviceAccountName on the Deployment", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
		})
	})
})
