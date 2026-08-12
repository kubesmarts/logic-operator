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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newDefReconciler() *LogicFlowDefinitionReconciler {
	return &LogicFlowDefinitionReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

func reconcileDefAndFetch(ctx context.Context, r *LogicFlowDefinitionReconciler, nn types.NamespacedName) *logicv1.LogicFlowDefinition {
	// First reconcile sets workflow labels (early return).
	// Second reconcile creates ConfigMap and updates status.
	for range 2 {
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
	}
	var def logicv1.LogicFlowDefinition
	Expect(k8sClient.Get(ctx, nn, &def)).To(Succeed())
	return &def
}

func validFlowJSON(name, version, namespace string) []byte {
	return []byte(`{"document":{"dsl":"1.0.0","namespace":"` + namespace + `","name":"` + name + `","version":"` + version + `"},"do":[{"noop":{"call":"http","with":{"method":"get","endpoint":"http://example.com"}}}]}`)
}

func createDefinition(ctx context.Context, name, runtimeRef string, flowJSON []byte) types.NamespacedName {
	nn := types.NamespacedName{Name: name, Namespace: testNamespace}
	// Delete first if exists to ensure clean state
	deleteDefinition(ctx, nn)
	def := &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: runtimeRef},
			Flow:       runtime.RawExtension{Raw: flowJSON},
		},
	}
	Expect(k8sClient.Create(ctx, def)).To(Succeed())
	return nn
}

func deleteDefinition(ctx context.Context, nn types.NamespacedName) {
	def := &logicv1.LogicFlowDefinition{}
	err := k8sClient.Get(ctx, nn, def)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, def)).To(Succeed())
}

func createRuntimeForDef(ctx context.Context, name string) types.NamespacedName {
	nn := types.NamespacedName{Name: name, Namespace: testNamespace}
	// Delete first if exists to ensure clean state
	deleteRuntime2(ctx, nn)
	rt := &logicv1.LogicFlowRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       logicv1.LogicFlowRuntimeSpec{},
	}
	Expect(k8sClient.Create(ctx, rt)).To(Succeed())
	return nn
}

func deleteRuntime2(ctx context.Context, nn types.NamespacedName) {
	rt := &logicv1.LogicFlowRuntime{}
	err := k8sClient.Get(ctx, nn, rt)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, rt)).To(Succeed())
}

var _ = Describe("LogicFlowDefinition Controller", func() {

	Context("Valid flow with existing Runtime", func() {
		const defName = "test-def-valid"
		const rtName = "test-def-rt"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("payment-processor", "1.0.0", "payments"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should create a ConfigMap with correct name and namespace", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())
			Expect(cm.Name).To(Equal("lfd-" + defName))
		})

		It("should set correct labels on the ConfigMap", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())

			Expect(cm.Labels).To(HaveKeyWithValue(testLabelKeyName, defName))
			Expect(cm.Labels).To(HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
			Expect(cm.Labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
			Expect(cm.Labels).To(HaveKeyWithValue(logicv1.LabelRuntimeRef, rtName))
			Expect(cm.Labels).To(HaveKeyWithValue(logicv1.LabelWorkflowName, "payment-processor"))
			Expect(cm.Labels).To(HaveKeyWithValue(logicv1.LabelWorkflowVersion, "1.0.0"))
		})

		It("should set owner references on the ConfigMap", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())

			Expect(cm.OwnerReferences).To(HaveLen(1))
			Expect(cm.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowDefinitionKind))
			Expect(cm.OwnerReferences[0].Name).To(Equal(defName))
			Expect(cm.OwnerReferences[0].UID).To(Equal(def.UID))
			Expect(*cm.OwnerReferences[0].Controller).To(BeTrue())
			Expect(*cm.OwnerReferences[0].BlockOwnerDeletion).To(BeTrue())
		})

		It("should set the flow data under the workflow name key", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())

			Expect(cm.Data).To(HaveKey("payment-processor.yaml"))
			Expect(cm.Data["payment-processor.yaml"]).To(ContainSubstring("name: payment-processor"))
		})

		It("should set status fields on first reconcile", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			Expect(def.Status.ObservedGeneration).To(Equal(def.Generation))
			Expect(def.Status.WorkflowName).To(Equal("payment-processor"))
			Expect(def.Status.WorkflowVersion).To(Equal("1.0.0"))
			Expect(def.Status.WorkflowNamespace).To(Equal(testNamespace))
			Expect(def.Status.ConfigMapRef).NotTo(BeNil())
			Expect(def.Status.ConfigMapRef.Name).To(Equal(ConfigMapPrefix + defName))
		})

		It("should set all conditions to True", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			rtCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionTrue))

			flowCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionFlowParsed)
			Expect(flowCond).NotTo(BeNil())
			Expect(flowCond.Status).To(Equal(metav1.ConditionTrue))

			cmCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionConfigMapReady)
			Expect(cmCond).NotTo(BeNil())
			Expect(cmCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("RuntimeRef not found", func() {
		const defName = "test-def-no-rt"
		var defNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			defNN = createDefinition(ctx, defName, "nonexistent-runtime", validFlowJSON("wf", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
		})

		It("should not create a ConfigMap", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			err := k8sClient.Get(ctx, cmNN, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should set RuntimeRefValid condition to False", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			rtCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(rtCond.Reason).To(Equal(logicv1.ReasonRuntimeNotFound))
		})
	})

	Context("Invalid flow JSON", func() {
		const defName = "test-def-bad-flow"
		const rtName = "test-def-rt-bad"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			// JSON that is structurally valid but missing required workflow fields
			defNN = createDefinition(ctx, defName, rtName, []byte(`{"not":"a workflow"}`))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should not create a ConfigMap", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			err := k8sClient.Get(ctx, cmNN, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should set FlowParsed condition to False", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			flowCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionFlowParsed)
			Expect(flowCond).NotTo(BeNil())
			Expect(flowCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(flowCond.Reason).To(Equal(logicv1.ReasonParseError))
		})

		It("should set RuntimeRefValid to True (runtime exists)", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			rtCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Spec update via SSA", func() {
		const defName = "test-def-ssa"
		const rtName = "test-def-rt-ssa"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("wf-v1", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should update the ConfigMap when flow changes", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var def logicv1.LogicFlowDefinition
			Expect(k8sClient.Get(ctx, defNN, &def)).To(Succeed())
			def.Spec.Flow = runtime.RawExtension{Raw: validFlowJSON("wf-v2", "2.0.0", "ns2")}
			Expect(k8sClient.Update(ctx, &def)).To(Succeed())

			updated := reconcileDefAndFetch(ctx, r, defNN)

			Expect(updated.Status.WorkflowName).To(Equal("wf-v2"))
			Expect(updated.Status.WorkflowVersion).To(Equal("2.0.0"))
			Expect(updated.Status.WorkflowNamespace).To(Equal(testNamespace))

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("wf-v2.yaml"))
			Expect(cm.Labels[logicv1.LabelWorkflowName]).To(Equal("wf-v2"))
			Expect(cm.Labels[logicv1.LabelWorkflowVersion]).To(Equal("2.0.0"))
		})

		It("should update ObservedGeneration after spec change", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)
			gen1 := def.Status.ObservedGeneration

			Expect(k8sClient.Get(ctx, defNN, def)).To(Succeed())
			def.Spec.Flow = runtime.RawExtension{Raw: validFlowJSON("wf-v3", "3.0.0", "ns")}
			Expect(k8sClient.Update(ctx, def)).To(Succeed())

			def = reconcileDefAndFetch(ctx, r, defNN)
			Expect(def.Status.ObservedGeneration).To(BeNumerically(">", gen1))
			Expect(def.Status.ObservedGeneration).To(Equal(def.Generation))
		})
	})

	Context("Idempotency", func() {
		const defName = "test-def-idempotent"
		const rtName = "test-def-rt-idem"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("wf", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should produce the same result on repeated reconciliation", func() {
			def1 := reconcileDefAndFetch(ctx, r, defNN)
			gen1 := def1.Status.ObservedGeneration
			condCount1 := len(def1.Status.Conditions)

			def2 := reconcileDefAndFetch(ctx, r, defNN)
			Expect(def2.Status.ObservedGeneration).To(Equal(gen1))
			Expect(def2.Status.Conditions).To(HaveLen(condCount1))
			Expect(def2.Status.WorkflowName).To(Equal(def1.Status.WorkflowName))
		})
	})

	Context("CR not found", func() {
		It("should return success for a missing CR", func() {
			r := newDefReconciler()
			nn := types.NamespacedName{Name: "does-not-exist-def", Namespace: testNamespace}

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var cm corev1.ConfigMap
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ConfigMapPrefix + "does-not-exist-def", Namespace: testNamespace}, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
