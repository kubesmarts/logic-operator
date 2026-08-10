package controller

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Cross-controller integration", func() {

	Context("Definition creates ConfigMap that Runtime discovers", func() {
		const rtName = "integ-rt"
		const defName = "integ-def"
		var rtNN, defNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("payment-processor", "1.0.0", "payments"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime(ctx, rtNN)
		})

		It("should mount the Definition's ConfigMap as a volume on the Runtime Deployment", func() {
			// Step 1: Reconcile Definition — creates the labeled ConfigMap
			def := reconcileDefAndFetch(ctx, defRec, defNN)
			Expect(def.Status.ConfigMapRef).NotTo(BeNil())
			cmName := def.Status.ConfigMapRef.Name

			// Verify ConfigMap exists with runtime-ref label
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Labels[LabelRuntimeRef]).To(Equal(rtName))

			// Step 2: Reconcile Runtime — discovers the ConfigMap
			rt := reconcileAndFetch(ctx, rtRec, rtNN)

			// Verify volume mounted
			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())

			vol := findVolume(&dep, cmName)
			Expect(vol).NotTo(BeNil(), "expected volume for ConfigMap %s", cmName)
			Expect(vol.ConfigMap.Name).To(Equal(cmName))

			vm := findVolumeMount(mainContainer(&dep), cmName)
			Expect(vm).NotTo(BeNil(), "expected volumeMount for ConfigMap %s", cmName)
			Expect(vm.MountPath).To(Equal(WorkflowMountPath + "/" + cmName))
			Expect(vm.ReadOnly).To(BeTrue())

			// Verify Runtime status
			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Name).To(Equal("payment-processor"))
			Expect(rt.Status.Definitions[0].Version).To(Equal("1.0.0"))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(1))
			Expect(rt.Status.ConfigMapRefs[0].Name).To(Equal(cmName))
		})

		It("should set QUARKUS_FLOW_RUNNER_SOURCE_PATH on the Runtime Deployment", func() {
			reconcileDefAndFetch(ctx, defRec, defNN)
			reconcileAndFetch(ctx, rtRec, rtNN)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			env := findEnvVar(mainContainer(&dep).Env, "QUARKUS_FLOW_RUNNER_SOURCE_PATH")
			Expect(env).NotTo(BeNil())
			Expect(env.Value).To(Equal(WorkflowMountPath))
		})
	})

	Context("Multiple Definitions targeting the same Runtime", func() {
		const rtName = "integ-multi-rt"
		const def1Name = "integ-def-order"
		const def2Name = "integ-def-payment"
		var rtNN, def1NN, def2NN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
			def1NN = createDefinition(ctx, def1Name, rtName, validFlowJSON("order-flow", "2.0.0", "orders"))
			def2NN = createDefinition(ctx, def2Name, rtName, validFlowJSON("payment-processor", "1.0.0", "payments"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, def1NN)
			deleteDefinition(ctx, def2NN)
			deleteRuntime(ctx, rtNN)
		})

		It("should mount both ConfigMaps sorted by name", func() {
			def1 := reconcileDefAndFetch(ctx, defRec, def1NN)
			def2 := reconcileDefAndFetch(ctx, defRec, def2NN)
			cm1Name := def1.Status.ConfigMapRef.Name
			cm2Name := def2.Status.ConfigMapRef.Name

			rt := reconcileAndFetch(ctx, rtRec, rtNN)

			// Verify both definitions in status, sorted by ConfigMap name
			Expect(rt.Status.Definitions).To(HaveLen(2))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(2))

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())

			var flowVols []string
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if strings.HasPrefix(v.Name, ConfigMapPrefix) {
					flowVols = append(flowVols, v.Name)
				}
			}
			Expect(flowVols).To(HaveLen(2))
			// Sorted by ConfigMap name
			Expect(flowVols[0] < flowVols[1]).To(BeTrue(), "volumes should be sorted: %v", flowVols)

			// Both have volume mounts
			Expect(findVolumeMount(mainContainer(&dep), cm1Name)).NotTo(BeNil())
			Expect(findVolumeMount(mainContainer(&dep), cm2Name)).NotTo(BeNil())
		})

		It("should populate status with metadata from both workflows", func() {
			reconcileDefAndFetch(ctx, defRec, def1NN)
			reconcileDefAndFetch(ctx, defRec, def2NN)
			rt := reconcileAndFetch(ctx, rtRec, rtNN)

			names := make(map[string]string)
			for _, d := range rt.Status.Definitions {
				names[d.Name] = d.Version
			}
			Expect(names).To(HaveKeyWithValue("order-flow", "2.0.0"))
			Expect(names).To(HaveKeyWithValue("payment-processor", "1.0.0"))
		})
	})

	Context("Definition update propagation", func() {
		const rtName = "integ-update-rt"
		const defName = "integ-update-def"
		var rtNN, defNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("my-flow", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime(ctx, rtNN)
		})

		It("should reflect updated workflow metadata in Runtime status after re-reconcile", func() {
			// Initial reconcile
			reconcileDefAndFetch(ctx, defRec, defNN)
			rt := reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Version).To(Equal("1.0.0"))

			// Update the Definition's flow spec
			var def logicv1.LogicFlowDefinition
			Expect(k8sClient.Get(ctx, defNN, &def)).To(Succeed())
			def.Spec.Flow.Raw = validFlowJSON("my-flow", "2.0.0", "ns")
			Expect(k8sClient.Update(ctx, &def)).To(Succeed())

			// Re-reconcile Definition — ConfigMap labels updated
			reconcileDefAndFetch(ctx, defRec, defNN)

			// Re-reconcile Runtime — picks up new labels
			rt = reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Name).To(Equal("my-flow"))
			Expect(rt.Status.Definitions[0].Version).To(Equal("2.0.0"))
		})
	})

	Context("Definition removal", func() {
		const rtName = "integ-remove-rt"
		const defName = "integ-remove-def"
		var rtNN, defNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("remove-flow", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteRuntime(ctx, rtNN)
		})

		It("should clean up volumes and status when Definition's ConfigMap is removed", func() {
			// Reconcile both — volume mounted, status populated
			def := reconcileDefAndFetch(ctx, defRec, defNN)
			cmName := def.Status.ConfigMapRef.Name
			rt := reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(HaveLen(1))

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).NotTo(BeNil())

			// Delete the Definition and its ConfigMap (simulating GC via owner ref)
			deleteDefinition(ctx, defNN)
			deleteConfigMap(ctx, cmName)

			// Re-reconcile Runtime
			rt = reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())

			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).To(BeNil())
		})
	})

	Context("Definition with invalid runtimeRef does not affect Runtime", func() {
		const rtName = "integ-invalid-rt"
		const validDefName = "integ-valid-def"
		const invalidDefName = "integ-invalid-def"
		var rtNN, validDefNN, invalidDefNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
			validDefNN = createDefinition(ctx, validDefName, rtName, validFlowJSON("valid-flow", "1.0.0", "ns"))
			invalidDefNN = createDefinition(ctx, invalidDefName, "nonexistent-runtime", validFlowJSON("invalid-flow", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, validDefNN)
			deleteDefinition(ctx, invalidDefNN)
			deleteRuntime(ctx, rtNN)
		})

		It("should only mount the valid Definition's ConfigMap", func() {
			// The invalid Definition's reconcile should set RuntimeRefValid=False and not create a ConfigMap
			invalidDef := reconcileDefAndFetch(ctx, defRec, invalidDefNN)
			rtCond := meta.FindStatusCondition(invalidDef.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionFalse))

			// No ConfigMap created for the invalid definition
			invalidCmNN := types.NamespacedName{Name: ConfigMapPrefix + invalidDefName, Namespace: "default"}
			var cm corev1.ConfigMap
			err := k8sClient.Get(ctx, invalidCmNN, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// Valid Definition creates its ConfigMap
			validDef := reconcileDefAndFetch(ctx, defRec, validDefNN)
			Expect(validDef.Status.ConfigMapRef).NotTo(BeNil())

			// Runtime only sees the valid one
			rt := reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Name).To(Equal("valid-flow"))
		})
	})

	Context("Runtime reconciled before any Definitions exist", func() {
		const rtName = "integ-empty-rt"
		const defName = "integ-late-def"
		var rtNN, defNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime(ctx, rtNN)
		})

		It("should start empty then pick up Definitions added later", func() {
			// Runtime reconciles with no definitions
			rt := reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Volumes).To(BeEmpty())

			// Now create a Definition
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("late-flow", "1.0.0", "ns"))
			def := reconcileDefAndFetch(ctx, defRec, defNN)
			cmName := def.Status.ConfigMapRef.Name

			// Re-reconcile Runtime
			rt = reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Name).To(Equal("late-flow"))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(1))

			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).NotTo(BeNil())
		})
	})

	Context("Idempotency across both controllers", func() {
		const rtName = "integ-idem-rt"
		const defName = "integ-idem-def"
		var rtNN, defNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, logicv1.LogicFlowRuntimeSpec{})
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("idem-flow", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime(ctx, rtNN)
		})

		It("should produce the same result on repeated reconciliation of both controllers", func() {
			// First pass
			reconcileDefAndFetch(ctx, defRec, defNN)
			rt1 := reconcileAndFetch(ctx, rtRec, rtNN)

			// Second pass — reconcile both again
			reconcileDefAndFetch(ctx, defRec, defNN)
			rt2 := reconcileAndFetch(ctx, rtRec, rtNN)

			Expect(rt2.Status.Definitions).To(HaveLen(1))
			Expect(rt2.Status.Definitions[0].Name).To(Equal(rt1.Status.Definitions[0].Name))
			Expect(rt2.Status.Definitions[0].Version).To(Equal(rt1.Status.Definitions[0].Version))
			Expect(rt2.Status.ConfigMapRefs).To(HaveLen(1))
			Expect(rt2.Status.ConfigMapRefs[0].Name).To(Equal(rt1.Status.ConfigMapRefs[0].Name))

			// Third pass — just Runtime again
			rt3 := reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt3.Status.Definitions[0]).To(Equal(rt2.Status.Definitions[0]))

			// Verify no condition count drift
			_, err := rtRec.Reconcile(ctx, reconcile.Request{NamespacedName: rtNN})
			Expect(err).NotTo(HaveOccurred())
			var rt4 logicv1.LogicFlowRuntime
			Expect(k8sClient.Get(ctx, rtNN, &rt4)).To(Succeed())
			Expect(rt4.Status.Conditions).To(HaveLen(len(rt3.Status.Conditions)))
		})
	})

	Context("Durable leases with ConfigMap integration", func() {
		const rtName = "integ-durable-rt"
		const defName = "integ-durable-def"
		var rtNN, defNN types.NamespacedName
		var rtRec *LogicFlowRuntimeReconciler
		var defRec *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			rtRec = newReconciler()
			defRec = newDefReconciler()
			rtNN = createRuntime(ctx, rtName, persistenceSpec())
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteLeases(ctx, rtName)

			deleteRuntime(ctx, rtNN)
		})

		It("should create leases, mount ConfigMaps, and set durable env vars", func() {
			// Step 1: Reconcile Runtime — leases created, durable env vars set
			rt := reconcileAndFetch(ctx, rtRec, rtNN)
			leases := listLeases(ctx, rtName)
			Expect(leases).To(HaveLen(1))
			Expect(rt.Status.LeaseReplicas).To(Equal(int32(1)))

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			c := mainContainer(&dep)
			Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED")).NotTo(BeNil())
			Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME").Value).To(Equal(rtName))

			var sa corev1.ServiceAccount
			Expect(k8sClient.Get(ctx, rtNN, &sa)).To(Succeed())
			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(rtName))

			// Step 2: Add a Definition — ConfigMap mounted alongside leases
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("durable-flow", "1.0.0", "ns"))
			def := reconcileDefAndFetch(ctx, defRec, defNN)
			cmName := def.Status.ConfigMapRef.Name

			rt = reconcileAndFetch(ctx, rtRec, rtNN)
			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).NotTo(BeNil())

			leases = listLeases(ctx, rtName)
			Expect(leases).To(HaveLen(1))

			// Step 3: Scale up — new leases created
			Expect(k8sClient.Get(ctx, rtNN, rt)).To(Succeed())
			replicas := int32(3)
			rt.Spec.Replicas = &replicas
			Expect(k8sClient.Update(ctx, rt)).To(Succeed())

			rt2 := reconcileAndFetch(ctx, rtRec, rtNN)
			leases = listLeases(ctx, rtName)
			Expect(leases).To(HaveLen(3))
			Expect(rt2.Status.LeaseReplicas).To(Equal(int32(3)))

			Expect(rt2.Status.Definitions).To(HaveLen(1))

			// Verify lease names follow convention
			for i := int32(0); i < 3; i++ {
				expectedName := fmt.Sprintf(LeaseMemberNameFmt, rtName, i)
				found := false
				for _, l := range leases {
					if l.Name == expectedName {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "expected lease %s not found", expectedName)
			}
		})
	})
})
