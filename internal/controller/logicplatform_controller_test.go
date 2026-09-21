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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func reconcilePlatformAndFetch(ctx context.Context, r *LogicPlatformReconciler, nn types.NamespacedName) *logicv1.LogicPlatform {
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
	Expect(err).NotTo(HaveOccurred())
	var plat logicv1.LogicPlatform
	Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
	return &plat
}

func newPlatformReconciler() *LogicPlatformReconciler {
	return &LogicPlatformReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

func createPlatform(ctx context.Context, name string, spec logicv1.LogicPlatformSpec) types.NamespacedName {
	nn := types.NamespacedName{Name: name, Namespace: testNamespace}
	plat := &logicv1.LogicPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       spec,
	}
	Expect(k8sClient.Create(ctx, plat)).To(Succeed())
	return nn
}

func deletePlatform(ctx context.Context, nn types.NamespacedName) {
	plat := &logicv1.LogicPlatform{}
	err := k8sClient.Get(ctx, nn, plat)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, plat)).To(Succeed())
}

func dataIndexContainer(dep *appsv1.Deployment) corev1.Container {
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == ContainerNameDataIndex {
			return c
		}
	}
	Fail("container " + ContainerNameDataIndex + " not found in Deployment")
	return corev1.Container{}
}

func platformSpec() logicv1.LogicPlatformSpec {
	return logicv1.LogicPlatformSpec{
		DataIndex: logicv1.DataIndexSpec{
			Enabled: true,
			Application: logicv1.ApplicationSpec{
				// Set image explicitly since webhook defaulter doesn't run in envtest
				Image:    logicv1.DefaultDataIndexImage(),
				Replicas: ptr.To(int32(1)),
			},
			Persistence: &logicv1.PersistenceOptionsSpec{
				PostgreSQL: &logicv1.PersistencePostgreSQL{
					SecretRef: logicv1.PostgreSQLSecretOptions{Name: "postgres-secret"},
					ServiceRef: &logicv1.PostgreSQLServiceOptions{
						SQLServiceOptions: &logicv1.SQLServiceOptions{
							Name: "postgres",
						},
						DatabaseSchema: "data-index",
					},
				},
			},
		},
	}
}

var _ = Describe("LogicPlatform Controller", func() {

	Context("Data Index with default configuration", func() {
		const name = "test-platform-default"
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
			nn = createPlatform(ctx, name, platformSpec())
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should create a Deployment with Data Index image and environment variables", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			c := dataIndexContainer(&dep)
			expectedImage := fmt.Sprintf("%s/%s:%s-%s", DataIndexRegistry, DataIndexImage, DataIndexVersion, logicv1.DataIndexVariant)
			Expect(c.Image).To(Equal(expectedImage))

			// Check Flyway migration environment variables
			flywayMigrate := findEnvVar(c.Env, "QUARKUS_FLYWAY_MIGRATE_AT_START")
			Expect(flywayMigrate).NotTo(BeNil())
			Expect(flywayMigrate.Value).To(Equal("true"))

			flywayBaseline := findEnvVar(c.Env, "QUARKUS_FLYWAY_BASELINE_ON_MIGRATE")
			Expect(flywayBaseline).NotTo(BeNil())
			Expect(flywayBaseline.Value).To(Equal("true"))

			// Check GraphQL environment variables
			graphqlUI := findEnvVar(c.Env, "QUARKUS_SMALLRYE_GRAPHQL_UI_ENABLE")
			Expect(graphqlUI).NotTo(BeNil())
			Expect(graphqlUI.Value).To(Equal("true"))

			// Check persistence environment variables
			dbKind := findEnvVar(c.Env, "QUARKUS_DATASOURCE_DB_KIND")
			Expect(dbKind).NotTo(BeNil())
			Expect(dbKind.Value).To(Equal("postgresql"))
		})

		It("should create a Deployment with default probes", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			c := dataIndexContainer(&dep)

			Expect(c.LivenessProbe).NotTo(BeNil())
			Expect(c.LivenessProbe.HTTPGet.Path).To(Equal("/q/health/live"))
			Expect(c.LivenessProbe.HTTPGet.Port.IntValue()).To(Equal(int(QuarkusPort)))

			Expect(c.ReadinessProbe).NotTo(BeNil())
			Expect(c.ReadinessProbe.HTTPGet.Path).To(Equal("/q/health/ready"))
			Expect(c.ReadinessProbe.HTTPGet.Port.IntValue()).To(Equal(int(QuarkusPort)))
		})

		It("should create a Deployment with default replicas", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			Expect(dep.Spec.Replicas).NotTo(BeNil())
			Expect(*dep.Spec.Replicas).To(Equal(int32(1)))
		})

		It("should create a Service with port 80 targeting 8080", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

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
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.OwnerReferences).To(HaveLen(1))
			Expect(dep.OwnerReferences[0].APIVersion).To(Equal(logicv1.GroupVersion.String()))
			Expect(dep.OwnerReferences[0].Kind).To(Equal(logicv1.LogicPlatformKind))
			Expect(dep.OwnerReferences[0].Name).To(Equal(name))
			Expect(dep.OwnerReferences[0].UID).To(Equal(plat.UID))
			Expect(*dep.OwnerReferences[0].Controller).To(BeTrue())
			Expect(*dep.OwnerReferences[0].BlockOwnerDeletion).To(BeTrue())

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, nn, &svc)).To(Succeed())
			Expect(svc.OwnerReferences).To(HaveLen(1))
			Expect(svc.OwnerReferences[0].Kind).To(Equal(logicv1.LogicPlatformKind))
			Expect(svc.OwnerReferences[0].UID).To(Equal(plat.UID))
		})

		It("should set deployment and service labels correctly", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(dep.Labels).To(HaveKeyWithValue(testLabelKeyName, name))
			Expect(dep.Labels).To(HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
			Expect(dep.Labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", LabelPartOf))
			Expect(dep.Spec.Selector.MatchLabels).To(Equal(SelectorLabels(name)))

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, nn, &svc)).To(Succeed())
			Expect(svc.Labels).To(HaveKeyWithValue(testLabelKeyName, name))
			Expect(svc.Labels).To(HaveKeyWithValue(testLabelKeyManagedBy, LabelManagedBy))
		})
	})

	Context("Data Index status updates", func() {
		const name = "test-platform-status"
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
			nn = createPlatform(ctx, name, platformSpec())
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should set status fields on first reconcile", func() {
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			Expect(plat.Status.ObservedGeneration).To(Equal(plat.Generation))
			Expect(plat.Status.DataIndex.Service.DeploymentRef.Name).To(Equal(name))
			Expect(plat.Status.DataIndex.Service.ServiceRef.Name).To(Equal(name))

			// Check replica counts (deployment just created, no replicas ready yet)
			Expect(plat.Status.DataIndex.Service.Replicas.Desired).To(Equal(int32(1)))
			Expect(plat.Status.DataIndex.Service.Replicas.Current).To(Equal(int32(0)))
			Expect(plat.Status.DataIndex.Service.Replicas.Ready).To(Equal(int32(0)))

			// Check conditions
			depCond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDataIndexDeploymentAvailable)
			Expect(depCond).NotTo(BeNil())
			Expect(depCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(depCond.Reason).To(Equal(logicv1.ReasonDeploymentProgressing))

			svcCond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDataIndexServiceReady)
			Expect(svcCond).NotTo(BeNil())
			Expect(svcCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(svcCond.Reason).To(Equal(logicv1.ReasonReady))

			// Check phase
			Expect(plat.Status.Phase).To(Equal(logicv1.LogicPlatformStatusPhase(logicv1.ApplicationPhasePending)))

			// Check Ready flag
			Expect(plat.Status.DataIndex.Service.Ready).To(BeFalse())
		})

		It("should set GraphQL and Metrics endpoints", func() {
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			expectedBaseURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", name, testNamespace, QuarkusPort)
			expectedGraphQLEndpoint := fmt.Sprintf("%s/graphql", expectedBaseURL)
			expectedMetricsEndpoint := fmt.Sprintf("%s/q/metrics", expectedBaseURL)

			Expect(plat.Status.DataIndex.Service.GraphQLEndpoint).To(Equal(expectedGraphQLEndpoint))
			Expect(plat.Status.DataIndex.Service.MetricsEndpoint).To(Equal(expectedMetricsEndpoint))
		})
	})

	Context("Data Index with custom replicas", func() {
		const name = "test-platform-replicas"
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
			spec := platformSpec()
			spec.DataIndex.Application.Replicas = ptr.To(int32(3))
			nn = createPlatform(ctx, name, spec)
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should create a Deployment with custom replica count", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			Expect(dep.Spec.Replicas).NotTo(BeNil())
			Expect(*dep.Spec.Replicas).To(Equal(int32(3)))
		})

		It("should set desired replicas in status to custom value", func() {
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			Expect(plat.Status.DataIndex.Service.Replicas.Desired).To(Equal(int32(3)))
		})
	})

	Context("Data Index with custom image", func() {
		const name = "test-platform-custom-image"
		var nn types.NamespacedName
		var r *LogicPlatformReconciler
		const customImage = "my-registry/custom-data-index:v1.0.0"

		BeforeEach(func() {
			r = newPlatformReconciler()
			spec := platformSpec()
			spec.DataIndex.Application.Image = customImage
			nn = createPlatform(ctx, name, spec)
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should use custom image instead of default", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			c := dataIndexContainer(&dep)
			Expect(c.Image).To(Equal(customImage))
		})
	})

	Context("Data Index without persistence", func() {
		const name = "test-platform-no-persist"
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
			spec := logicv1.LogicPlatformSpec{
				DataIndex: logicv1.DataIndexSpec{
					Enabled: true,
					Application: logicv1.ApplicationSpec{
						Image:    logicv1.DefaultDataIndexImage(),
						Replicas: ptr.To(int32(1)),
					},
					Persistence: nil, // No persistence configured
				},
			}
			nn = createPlatform(ctx, name, spec)
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should not include ServiceAccount when persistence is not configured", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
		})

		It("should not include persistence environment variables", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			c := dataIndexContainer(&dep)
			dbKind := findEnvVar(c.Env, "QUARKUS_DATASOURCE_DB_KIND")
			Expect(dbKind).To(BeNil())
		})

		It("should still include GraphQL and Flyway environment variables", func() {
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			c := dataIndexContainer(&dep)

			graphqlUI := findEnvVar(c.Env, "QUARKUS_SMALLRYE_GRAPHQL_UI_ENABLE")
			Expect(graphqlUI).NotTo(BeNil())
			Expect(graphqlUI.Value).To(Equal("true"))

			flywayMigrate := findEnvVar(c.Env, "QUARKUS_FLYWAY_MIGRATE_AT_START")
			Expect(flywayMigrate).NotTo(BeNil())
		})
	})

	Context("Reconcile updates", func() {
		const name = "test-platform-update"
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
			nn = createPlatform(ctx, name, platformSpec())
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should update deployment when spec changes", func() {
			// Initial reconcile
			reconcilePlatformAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			originalReplicas := *dep.Spec.Replicas
			Expect(originalReplicas).To(Equal(int32(1)))

			// Update the platform spec
			var plat logicv1.LogicPlatform
			Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
			plat.Spec.DataIndex.Application.Replicas = ptr.To(int32(5))
			Expect(k8sClient.Update(ctx, &plat)).To(Succeed())

			// Reconcile again
			reconcilePlatformAndFetch(ctx, r, nn)

			// Check deployment was updated
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(*dep.Spec.Replicas).To(Equal(int32(5)))
		})

		It("should update status.observedGeneration when spec changes", func() {
			// Initial reconcile
			plat1 := reconcilePlatformAndFetch(ctx, r, nn)
			gen1 := plat1.Generation
			observedGen1 := plat1.Status.ObservedGeneration
			Expect(observedGen1).To(Equal(gen1))

			// Update the platform spec
			var plat logicv1.LogicPlatform
			Expect(k8sClient.Get(ctx, nn, &plat)).To(Succeed())
			plat.Spec.DataIndex.Application.Replicas = ptr.To(int32(2))
			Expect(k8sClient.Update(ctx, &plat)).To(Succeed())

			// Reconcile again
			plat2 := reconcilePlatformAndFetch(ctx, r, nn)
			gen2 := plat2.Generation
			observedGen2 := plat2.Status.ObservedGeneration

			// Generation should increase
			Expect(gen2).To(BeNumerically(">", gen1))
			// ObservedGeneration should match new generation
			Expect(observedGen2).To(Equal(gen2))
		})
	})

	Context("Ingress configuration", func() {
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should create Ingress when enabled with host", func() {
			name := "test-platform-ingress-enabled"
			spec := platformSpec()
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled: true,
				Host:    "data-index.example.com",
			}
			nn = createPlatform(ctx, name, spec)

			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// Check IngressRef in status
			Expect(plat.Status.IngressRef).NotTo(BeNil())
			Expect(plat.Status.IngressRef.Name).To(Equal(name))
			Expect(plat.Status.RouteRef).To(BeNil())

			// Check Ingress resource exists
			var ing networkingv1.Ingress
			Expect(k8sClient.Get(ctx, nn, &ing)).To(Succeed())
			Expect(ing.Spec.Rules).To(HaveLen(1))
			Expect(ing.Spec.Rules[0].Host).To(Equal("data-index.example.com"))
		})

		It("should not create Ingress when disabled", func() {
			name := "test-platform-ingress-disabled"
			spec := platformSpec()
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled: false,
				Host:    "data-index.example.com",
			}
			nn = createPlatform(ctx, name, spec)

			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// Check status has no ingress refs
			Expect(plat.Status.IngressRef).To(BeNil())
			Expect(plat.Status.RouteRef).To(BeNil())

			// Check Ingress resource does not exist
			var ing networkingv1.Ingress
			err := k8sClient.Get(ctx, nn, &ing)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// Endpoints should use internal service URL when ingress is disabled
			Expect(plat.Status.DataIndex.Service.GraphQLEndpoint).To(ContainSubstring("svc.cluster.local"))
			Expect(plat.Status.DataIndex.Service.MetricsEndpoint).To(ContainSubstring("svc.cluster.local"))
		})

		It("should populate URL in status when Ingress enabled", func() {
			name := "test-platform-ingress-url"
			spec := platformSpec()
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled: true,
				Host:    "data-index.example.com",
			}
			nn = createPlatform(ctx, name, spec)

			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// URL should be populated (http without TLS)
			Expect(plat.Status.DataIndex.Service.URL).To(Equal("http://data-index.example.com"))

			// Endpoints should use external URL when ingress is enabled
			Expect(plat.Status.DataIndex.Service.GraphQLEndpoint).To(Equal("http://data-index.example.com/graphql"))
			Expect(plat.Status.DataIndex.Service.MetricsEndpoint).To(Equal("http://data-index.example.com/q/metrics"))
		})

		It("should configure TLS when enabled", func() {
			name := "test-platform-ingress-tls"
			spec := platformSpec()
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled: true,
				Host:    "data-index.example.com",
				TLS: logicv1.TLSSpec{
					Enabled: true,
					SecretRef: corev1.LocalObjectReference{
						Name: "data-index-tls",
					},
				},
			}
			nn = createPlatform(ctx, name, spec)

			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// URL and endpoints should use https
			Expect(plat.Status.DataIndex.Service.URL).To(Equal("https://data-index.example.com"))
			Expect(plat.Status.DataIndex.Service.GraphQLEndpoint).To(Equal("https://data-index.example.com/graphql"))
			Expect(plat.Status.DataIndex.Service.MetricsEndpoint).To(Equal("https://data-index.example.com/q/metrics"))

			// Check Ingress has TLS configuration
			var ing networkingv1.Ingress
			Expect(k8sClient.Get(ctx, nn, &ing)).To(Succeed())
			Expect(ing.Spec.TLS).To(HaveLen(1))
			Expect(ing.Spec.TLS[0].SecretName).To(Equal("data-index-tls"))
			Expect(ing.Spec.TLS[0].Hosts).To(ContainElement("data-index.example.com"))
		})

		It("should configure cert-manager annotations when TLS with certManager", func() {
			name := "test-platform-ingress-certmgr"
			spec := platformSpec()
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled: true,
				Host:    "data-index.example.com",
				TLS: logicv1.TLSSpec{
					Enabled: true,
					CertManager: &logicv1.CertManagerSpec{
						IssuerRef: logicv1.CertManagerIssuerRef{
							Name: "letsencrypt-prod",
							Kind: "ClusterIssuer",
						},
					},
				},
			}
			nn = createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			// Check Ingress has cert-manager annotations
			var ing networkingv1.Ingress
			Expect(k8sClient.Get(ctx, nn, &ing)).To(Succeed())
			Expect(ing.Annotations).To(HaveKeyWithValue("cert-manager.io/cluster-issuer", "letsencrypt-prod"))

			// Check TLS secret name is auto-generated
			Expect(ing.Spec.TLS).To(HaveLen(1))
			Expect(ing.Spec.TLS[0].SecretName).To(Equal(name + "-tls"))
		})

		It("should set custom IngressClassName when specified", func() {
			name := "test-platform-ingress-class"
			spec := platformSpec()
			className := "custom-ingress"
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled:          true,
				Host:             "data-index.example.com",
				IngressClassName: &className,
			}
			nn = createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var ing networkingv1.Ingress
			Expect(k8sClient.Get(ctx, nn, &ing)).To(Succeed())
			Expect(ing.Spec.IngressClassName).NotTo(BeNil())
			Expect(*ing.Spec.IngressClassName).To(Equal("custom-ingress"))
		})

		It("should apply custom annotations to Ingress", func() {
			name := "test-platform-ingress-annot"
			spec := platformSpec()
			spec.DataIndex.Ingress = &logicv1.DataIndexIngressSpec{
				Enabled: true,
				Host:    "data-index.example.com",
				Annotations: map[string]string{
					"custom-annotation": "custom-value",
					"another-one":       "another-value",
				},
			}
			nn = createPlatform(ctx, name, spec)

			reconcilePlatformAndFetch(ctx, r, nn)

			var ing networkingv1.Ingress
			Expect(k8sClient.Get(ctx, nn, &ing)).To(Succeed())
			Expect(ing.Annotations).To(HaveKeyWithValue("custom-annotation", "custom-value"))
			Expect(ing.Annotations).To(HaveKeyWithValue("another-one", "another-value"))
		})
	})

	Context("Persistence configuration validation", func() {
		var nn types.NamespacedName
		var r *LogicPlatformReconciler

		BeforeEach(func() {
			r = newPlatformReconciler()
		})
		AfterEach(func() {
			deletePlatform(ctx, nn)
		})

		It("should validate when secret and service exist", func() {
			name := "test-platform-persist-valid"

			// Create secret and service first with unique names
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persist-valid-secret",
					Namespace: testNamespace,
				},
				StringData: map[string]string{
					"username": "user",
					"password": "pass",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, secret)
			})

			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persist-valid-postgres",
					Namespace: testNamespace,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Port: 5432}},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, svc)
			})

			spec := platformSpec()
			spec.DataIndex.Persistence.PostgreSQL.SecretRef.Name = "persist-valid-secret"
			spec.DataIndex.Persistence.PostgreSQL.ServiceRef.Name = "persist-valid-postgres"
			nn = createPlatform(ctx, name, spec)
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// Check persistence config status
			Expect(plat.Status.DataIndex.Persistence).NotTo(BeNil())
			Expect(plat.Status.DataIndex.Persistence.Valid).To(BeTrue())
			Expect(plat.Status.DataIndex.Persistence.SecretExists).To(BeTrue())
			Expect(plat.Status.DataIndex.Persistence.ServiceExists).To(BeTrue())

			// Check condition
			cond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should report invalid when secret missing", func() {
			name := "test-platform-persist-no-secret"
			spec := platformSpec()
			spec.DataIndex.Persistence.PostgreSQL.SecretRef.Name = "missing-secret"
			spec.DataIndex.Persistence.PostgreSQL.ServiceRef.Name = "missing-postgres"
			nn = createPlatform(ctx, name, spec)
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// Check persistence config status
			Expect(plat.Status.DataIndex.Persistence).NotTo(BeNil())
			Expect(plat.Status.DataIndex.Persistence.Valid).To(BeFalse())
			Expect(plat.Status.DataIndex.Persistence.SecretExists).To(BeFalse())
			Expect(plat.Status.DataIndex.Persistence.Error).To(ContainSubstring("secret"))

			// Check condition
			cond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(logicv1.ReasonPersistenceConfigInvalid))
		})

		It("should report invalid when service missing", func() {
			name := "test-platform-persist-no-svc"

			// Create secret but not service
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "persist-no-svc-secret",
					Namespace: testNamespace,
				},
				StringData: map[string]string{
					"username": "user",
					"password": "pass",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, secret)
			})

			spec := platformSpec()
			spec.DataIndex.Persistence.PostgreSQL.SecretRef.Name = "persist-no-svc-secret"
			spec.DataIndex.Persistence.PostgreSQL.ServiceRef.Name = "persist-no-svc-postgres"
			nn = createPlatform(ctx, name, spec)
			plat := reconcilePlatformAndFetch(ctx, r, nn)

			// Check persistence config status
			Expect(plat.Status.DataIndex.Persistence).NotTo(BeNil())
			Expect(plat.Status.DataIndex.Persistence.Valid).To(BeFalse())
			Expect(plat.Status.DataIndex.Persistence.SecretExists).To(BeTrue())
			Expect(plat.Status.DataIndex.Persistence.ServiceExists).To(BeFalse())
			Expect(plat.Status.DataIndex.Persistence.Error).To(ContainSubstring("service"))

			// Check condition
			cond := meta.FindStatusCondition(plat.Status.Conditions, logicv1.ConditionDataIndexPersistenceReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})
	})
})
