package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
)

func testFlowRaw() runtime.RawExtension {
	return runtime.RawExtension{
		Raw: []byte(`{
			"document": {
				"dsl": "1.0.0",
				"namespace": "payments",
				"name": "payment",
				"version": "1.0.0"
			},
			"do": [
				{
					"step1": {
						"set": {
							"result": "ok"
						}
					}
				}
			]
		}`),
	}
}

var _ = Describe("LogicFlowService Controller", func() {
	const (
		svcName = "test-svc"
		rtName  = "test-svc-runtime"
		defName = "test-svc-def"
	)

	ctx := context.Background()

	svcNN := types.NamespacedName{Name: svcName, Namespace: testNamespace}
	rtNN := types.NamespacedName{Name: rtName, Namespace: testNamespace}
	defNN := types.NamespacedName{Name: defName, Namespace: testNamespace}

	BeforeEach(func() {
		By("creating the prerequisite LogicFlowRuntime")
		rt := &logicv1.LogicFlowRuntime{}
		err := k8sClient.Get(ctx, rtNN, rt)
		if err != nil && errors.IsNotFound(err) {
			rt = &logicv1.LogicFlowRuntime{
				ObjectMeta: metav1.ObjectMeta{Name: rtName, Namespace: testNamespace},
				Spec: logicv1.LogicFlowRuntimeSpec{
					RuntimeSpec: logicv1.RuntimeSpec{
						ApplicationSpec: logicv1.ApplicationSpec{
							Image: "quay.io/kubesmarts/quarkus-flow:0.15.1-minimal",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, rt)).To(Succeed())
		}

		By("creating the prerequisite LogicFlowDefinition")
		def := &logicv1.LogicFlowDefinition{}
		err = k8sClient.Get(ctx, defNN, def)
		if err != nil && errors.IsNotFound(err) {
			def = &logicv1.LogicFlowDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defName,
					Namespace: testNamespace,
					Labels: map[string]string{
						logicv1.LabelWorkflowName:      "payment",
						logicv1.LabelWorkflowNamespace: "payments",
						logicv1.LabelWorkflowVersion:   "1.0.0",
						logicv1.LabelRuntimeRef:        rtName,
					},
				},
				Spec: logicv1.LogicFlowDefinitionSpec{
					RuntimeRef: corev1.LocalObjectReference{Name: rtName},
					Flow:       testFlowRaw(),
				},
			}
			Expect(k8sClient.Create(ctx, def)).To(Succeed())
		}

		By("creating the LogicFlowService")
		svc := &logicv1.LogicFlowService{}
		err = k8sClient.Get(ctx, svcNN, svc)
		if err != nil && errors.IsNotFound(err) {
			svc = &logicv1.LogicFlowService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: testNamespace},
				Spec: logicv1.LogicFlowServiceSpec{
					DefaultDefinition: &corev1.LocalObjectReference{Name: defName},
					Ingress: logicv1.IngressSpec{
						Host: "test.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		}
	})

	AfterEach(func() {
		svc := &logicv1.LogicFlowService{}
		if err := k8sClient.Get(ctx, svcNN, svc); err == nil {
			Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
		}
		def := &logicv1.LogicFlowDefinition{}
		if err := k8sClient.Get(ctx, defNN, def); err == nil {
			Expect(k8sClient.Delete(ctx, def)).To(Succeed())
		}
		rt := &logicv1.LogicFlowRuntime{}
		if err := k8sClient.Get(ctx, rtNN, rt); err == nil {
			Expect(k8sClient.Delete(ctx, rt)).To(Succeed())
		}
	})

	It("should create an Ingress with rewrite-target on reconcile", func() {
		reconciler := &LogicFlowServiceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: svcNN})
		Expect(err).NotTo(HaveOccurred())

		var ingress networkingv1.Ingress
		Expect(k8sClient.Get(ctx, svcNN, &ingress)).To(Succeed())
		Expect(ingress.Spec.Rules).To(HaveLen(1))
		Expect(ingress.Spec.Rules[0].Host).To(Equal("test.example.com"))
		Expect(ingress.Spec.Rules[0].HTTP.Paths).To(HaveLen(1))
		Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
		Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(rtName))
		Expect(ingress.Annotations).To(HaveKeyWithValue(
			annotationNginxRewriteTarget,
			"/q/flow/exec/payments/payment/1.0.0",
		))
	})

})
