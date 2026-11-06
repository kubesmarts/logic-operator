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
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
				},
				Runtime: &logicv1.RuntimeDatastoreSpec{
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
})
