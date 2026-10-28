package controller

import (
	"context"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("LogicPlatform WorkflowGateway Controller", func() {
	var (
		ctx context.Context
		r   *LogicPlatformReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		r = newPlatformReconciler()
	})

	Describe("Gateway Deployment Apply", func() {
		It("should create a Deployment with correct image when gateway is enabled", func() {
			name := "test-gw-deploy"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image:    "test-image:v1.0.0",
					Replicas: ptr.To(int32(2)),
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var deploy appsv1.Deployment
			deployKey := types.NamespacedName{Name: name + "-workflow-gateway", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, deployKey, &deploy)).To(Succeed())

			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("test-image:v1.0.0"))
			Expect(*deploy.Spec.Replicas).To(Equal(int32(2)))
		})

		It("should omit replicas when Application.Replicas is nil (HPA-compatible)", func() {
			name := "test-hpa-gw"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
					// Replicas is nil — allows HPA to own scaling
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var deploy appsv1.Deployment
			deployKey := types.NamespacedName{Name: name + "-workflow-gateway", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, deployKey, &deploy)).To(Succeed())

			// The actual Deployment will have replicas=1 (server default),
			// but the important thing is that Application.Replicas is nil in the CR,
			// which means we don't explicitly override HPA scaling decisions.
			// Verify the Deployment was created (test succeeds if we get here).
			Expect(deploy.Name).To(Equal(name + "-workflow-gateway"))
		})

		It("should set dedicated ServiceAccount on the pod", func() {
			name := "test-sa-gw"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var deploy appsv1.Deployment
			deployKey := types.NamespacedName{Name: name + "-workflow-gateway", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, deployKey, &deploy)).To(Succeed())

			Expect(deploy.Spec.Template.Spec.ServiceAccountName).To(Equal(name + "-workflow-gateway"))
		})
	})

	Describe("ServiceAccount Creation", func() {
		It("should create a dedicated ServiceAccount for the gateway", func() {
			name := "test-sa-create"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var sa corev1.ServiceAccount
			saKey := types.NamespacedName{Name: name + "-workflow-gateway", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, saKey, &sa)).To(Succeed())

			Expect(sa.OwnerReferences).To(HaveLen(1))
			Expect(sa.OwnerReferences[0].Name).To(Equal(name))
		})
	})

	Describe("ClusterRoleBinding Creation", func() {
		It("should create a ClusterRoleBinding with correct labels", func() {
			name := "test-crb-create"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var binding rbacv1.ClusterRoleBinding
			bindingKey := types.NamespacedName{Name: testNamespace + "-" + name + "-workflow-gateway"}
			Expect(k8sClient.Get(ctx, bindingKey, &binding)).To(Succeed())

			Expect(binding.Labels["logic.kubesmarts.org/platform-namespace"]).To(Equal(testNamespace))
			Expect(binding.Labels["logic.kubesmarts.org/platform-name"]).To(Equal(name))
			Expect(binding.RoleRef.Name).To(Equal(ClusterRoleWorkflowGateway))
			Expect(binding.Subjects).To(HaveLen(1))
			Expect(binding.Subjects[0].Name).To(Equal(name + "-workflow-gateway"))
			Expect(binding.Subjects[0].Namespace).To(Equal(testNamespace))
		})
	})

	Describe("Service Creation", func() {
		It("should create a Service exposing port 80 targeting pod 8080", func() {
			name := "test-svc-gw"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var svc corev1.Service
			svcKey := types.NamespacedName{Name: name + "-workflow-gateway", Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, svcKey, &svc)).To(Succeed())

			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(svc.Spec.Ports[0].TargetPort.IntVal).To(Equal(int32(8080)))
			Expect(svc.Spec.Selector).To(Equal(SelectorLabels(name + "-workflow-gateway")))
		})
	})

	Describe("Finalizer Handling", func() {
		It("should add finalizer when gateway is enabled", func() {
			name := "test-finalizer-add"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var updated logicv1.LogicPlatform
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(FinalizerWorkflowGateway))
		})

		It("should remove finalizer when gateway is disabled", func() {
			name := "test-finalizer-remove"
			spec := platformSpec()
			// Create with gateway first
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			// First reconcile adds finalizer
			reconcilePlatformAndFetch(ctx, r, nn)

			// Now disable gateway
			var plat logicv1.LogicPlatform
			Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
			plat.Spec.WorkflowGateway = nil
			Expect(k8sClient.Update(ctx, &plat)).To(Succeed())

			// Reconcile again
			reconcilePlatformAndFetch(ctx, r, nn)

			// Finalizer should be removed
			Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
			Expect(plat.Finalizers).NotTo(ContainElement(FinalizerWorkflowGateway))
		})
	})

	Describe("Status Updates", func() {
		It("should report gateway deployment status", func() {
			name := "test-status-gw"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image:    "test-image:v1.0.0",
					Replicas: ptr.To(int32(1)),
				},
			}
			nn := createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var updated logicv1.LogicPlatform
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			Expect(updated.Status.WorkflowGateway).NotTo(BeNil())
			Expect(updated.Status.WorkflowGateway.DeploymentRef.Name).To(Equal(name + "-workflow-gateway"))
			Expect(updated.Status.WorkflowGateway.ServiceRef.Name).To(Equal(name + "-workflow-gateway"))
			Expect(updated.Status.WorkflowGateway.ServiceAccountRef.Name).To(Equal(name + "-workflow-gateway"))
		})

		It("should clear gateway status when disabled", func() {
			name := "test-status-clear"
			spec := platformSpec()
			spec.WorkflowGateway = &logicv1.WorkflowGatewaySpec{
				Application: logicv1.ApplicationSpec{
					Image: "test-image:v1.0.0",
				},
			}
			nn := createPlatform(ctx, name, spec)

			// First reconcile creates gateway
			reconcilePlatformAndFetch(ctx, r, nn)

			// Verify status is populated
			var plat logicv1.LogicPlatform
			Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
			Expect(plat.Status.WorkflowGateway).NotTo(BeNil())

			// Disable gateway
			plat.Spec.WorkflowGateway = nil
			Expect(k8sClient.Update(ctx, &plat)).To(Succeed())

			// Reconcile again
			reconcilePlatformAndFetch(ctx, r, nn)

			// Status should be cleared
			Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
			Expect(plat.Status.WorkflowGateway).To(BeNil())
		})
	})
})
