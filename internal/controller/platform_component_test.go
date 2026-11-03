package controller

import (
	"context"
	"errors"
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeComponent is a test double that records calls and can be configured to fail.
type fakeComponent struct {
	name            string
	reconcileErr    error
	statusErr       error
	reconcileCalled bool
	statusCalled    bool
}

func (fc *fakeComponent) Name() string { return fc.name }
func (fc *fakeComponent) Reconcile(_ context.Context, _ *logicv1.LogicPlatform) error {
	fc.reconcileCalled = true
	return fc.reconcileErr
}
func (fc *fakeComponent) UpdateStatus(_ context.Context, _ *logicv1.LogicPlatform) error {
	fc.statusCalled = true
	return fc.statusErr
}

// countingClient wraps a client.Client and counts calls to Status().Update.
type countingClient struct {
	client.Client
	statusUpdates int
}

func (c *countingClient) Status() client.SubResourceWriter {
	return &countingStatusWriter{SubResourceWriter: c.Client.Status(), parent: c}
}

type countingStatusWriter struct {
	client.SubResourceWriter
	parent *countingClient
}

func (w *countingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.parent.statusUpdates++
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}

// newTestPlatform builds a minimal LogicPlatform registered with the scheme.
func newTestPlatform(t *testing.T) (*logicv1.LogicPlatform, *runtime.Scheme) {
	t.Helper()
	testScheme := scheme.Scheme
	if err := logicv1.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add logicv1 to scheme: %v", err)
	}
	plat := &logicv1.LogicPlatform{}
	plat.Name = "test-platform"
	plat.Namespace = "default"
	return plat, testScheme
}

func platformRequest() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-platform", Namespace: "default"}}
}

func TestReconcile_RunsAllComponentsIndependently(t *testing.T) {
	// c1.Reconcile errors; c2 and c3 must still Reconcile and UpdateStatus.
	c1 := &fakeComponent{name: "Component1", reconcileErr: errors.New("c1 error")}
	c2 := &fakeComponent{name: "Component2"}
	c3 := &fakeComponent{name: "Component3"}

	plat, testScheme := newTestPlatform(t)
	plat.Spec.DataIndex.Enabled = true

	fakeCli := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(plat).
		WithStatusSubresource(plat).
		Build()

	r := &LogicPlatformReconciler{
		Client: fakeCli,
		Scheme: testScheme,
		platformComponents: func(_ *LogicPlatformReconciler) []PlatformComponent {
			return []PlatformComponent{c1, c2, c3}
		},
	}

	_, err := r.Reconcile(context.Background(), platformRequest())

	// Every component's Reconcile ran despite c1's error.
	for _, c := range []*fakeComponent{c1, c2, c3} {
		if !c.reconcileCalled {
			t.Errorf("%s.Reconcile was not called", c.name)
		}
		if !c.statusCalled {
			t.Errorf("%s.UpdateStatus was not called", c.name)
		}
	}

	// The reconcile returns an error that wraps c1's failure.
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !errors.Is(err, c1.reconcileErr) {
		t.Errorf("expected error to wrap c1 error, got: %v", err)
	}
}

func TestReconcile_AggregatesErrorsFromBothPhases(t *testing.T) {
	// c1.Reconcile errors, c2.UpdateStatus errors — both must surface.
	c1 := &fakeComponent{name: "Component1", reconcileErr: errors.New("c1 reconcile error")}
	c2 := &fakeComponent{name: "Component2", statusErr: errors.New("c2 status error")}

	plat, testScheme := newTestPlatform(t)

	fakeCli := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(plat).
		WithStatusSubresource(plat).
		Build()

	r := &LogicPlatformReconciler{
		Client: fakeCli,
		Scheme: testScheme,
		platformComponents: func(_ *LogicPlatformReconciler) []PlatformComponent {
			return []PlatformComponent{c1, c2}
		},
	}

	_, err := r.Reconcile(context.Background(), platformRequest())

	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !errors.Is(err, c1.reconcileErr) {
		t.Errorf("expected error to wrap c1 reconcile error, got: %v", err)
	}
	if !errors.Is(err, c2.statusErr) {
		t.Errorf("expected error to wrap c2 status error, got: %v", err)
	}
}

func TestReconcile_WritesStatusExactlyOnce(t *testing.T) {
	c1 := &fakeComponent{name: "Component1"}
	c2 := &fakeComponent{name: "Component2"}
	c3 := &fakeComponent{name: "Component3"}

	plat, testScheme := newTestPlatform(t)
	plat.Spec.DataIndex.Enabled = true

	base := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(plat).
		WithStatusSubresource(plat).
		Build()
	counting := &countingClient{Client: base}

	r := &LogicPlatformReconciler{
		Client: counting,
		Scheme: testScheme,
		platformComponents: func(_ *LogicPlatformReconciler) []PlatformComponent {
			return []PlatformComponent{c1, c2, c3}
		},
	}

	if _, err := r.Reconcile(context.Background(), platformRequest()); err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}

	// Despite three components, the orchestrator writes .status exactly once.
	if counting.statusUpdates != 1 {
		t.Errorf("expected exactly 1 Status().Update call, got %d", counting.statusUpdates)
	}
}
