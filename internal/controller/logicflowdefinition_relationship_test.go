package controller

import (
	"context"
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCheckServiceRelationship(t *testing.T) {
	const namespace = "workflows"
	tests := []struct {
		name     string
		services []logicv1.LogicFlowService
		want     string
	}{
		{
			name: "default definition reference",
			services: []logicv1.LogicFlowService{{
				ObjectMeta: metav1.ObjectMeta{Name: "default-service", Namespace: namespace},
				Spec:       logicv1.LogicFlowServiceSpec{DefaultDefinition: &corev1.LocalObjectReference{Name: "target"}},
			}},
			want: "default-service",
		},
		{
			name: "traffic definition reference",
			services: []logicv1.LogicFlowService{{
				ObjectMeta: metav1.ObjectMeta{Name: "traffic-service", Namespace: namespace},
				Spec: logicv1.LogicFlowServiceSpec{Traffic: []logicv1.TrafficSpec{{
					DefinitionRef: corev1.LocalObjectReference{Name: "target"},
					Weight:        100,
				}}},
			}},
			want: "traffic-service",
		},
		{
			name: "unreferenced definition",
			services: []logicv1.LogicFlowService{{
				ObjectMeta: metav1.ObjectMeta{Name: "other-service", Namespace: namespace},
				Spec:       logicv1.LogicFlowServiceSpec{DefaultDefinition: &corev1.LocalObjectReference{Name: "other"}},
			}},
		},
		{
			name: "ignores service in another namespace",
			services: []logicv1.LogicFlowService{{
				ObjectMeta: metav1.ObjectMeta{Name: "other-namespace-service", Namespace: "other"},
				Spec:       logicv1.LogicFlowServiceSpec{DefaultDefinition: &corev1.LocalObjectReference{Name: "target"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := logicv1.AddToScheme(scheme); err != nil {
				t.Fatalf("register API types: %v", err)
			}
			objects := make([]logicv1.LogicFlowService, len(tt.services))
			copy(objects, tt.services)
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			for i := range objects {
				clientBuilder = clientBuilder.WithObjects(&objects[i])
			}
			r := &LogicFlowDefinitionReconciler{Client: clientBuilder.Build()}
			def := &logicv1.LogicFlowDefinition{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: namespace}}

			got, err := r.checkServiceRelationship(context.Background(), def)
			if err != nil {
				t.Fatalf("checkServiceRelationship() error = %v", err)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("checkServiceRelationship() = %q, want nil", got.Name)
				}
				return
			}
			if got == nil || got.Name != tt.want {
				t.Fatalf("checkServiceRelationship() = %v, want service %q", got, tt.want)
			}
		})
	}
}
