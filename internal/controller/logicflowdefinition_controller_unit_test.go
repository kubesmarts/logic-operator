package controller

import (
	"context"
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNormalizeFlowDefinitionPersistsResourceNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := logicv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	def := &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "definition", Namespace: "workflows"},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: "runtime"},
			Flow:       runtime.RawExtension{Raw: []byte(`{"document":{"dsl":"1.0.0","namespace":"from-flow","name":"hello","version":"1.0.0"},"do":[{"noop":{"call":"http","with":{"method":"get","endpoint":"http://example.com"}}}]}`)},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(def).Build()
	reconciler := &LogicFlowDefinitionReconciler{Client: fakeClient}

	wf, err := def.Spec.ParseFlow()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := reconciler.normalizeFlowDefinition(context.Background(), def, wf)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected the first normalization to patch the definition")
	}

	var persisted logicv1.LogicFlowDefinition
	if err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(def), &persisted); err != nil {
		t.Fatal(err)
	}
	normalized, err := persisted.Spec.ParseFlow()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Document.Namespace != persisted.Namespace {
		t.Fatalf("flow namespace = %q, want CR namespace %q", normalized.Document.Namespace, persisted.Namespace)
	}
	if got := persisted.Labels[logicv1.LabelWorkflowNamespace]; got != persisted.Namespace {
		t.Fatalf("workflow namespace label = %q, want CR namespace %q", got, persisted.Namespace)
	}

	changed, err = reconciler.normalizeFlowDefinition(context.Background(), &persisted, normalized)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected normalized definition to be idempotent")
	}
}
