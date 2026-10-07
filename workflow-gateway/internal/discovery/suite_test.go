package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// testCfg is non-nil only when an envtest control plane started successfully.
// When nil, the envtest-backed tests skip so the pure unit tests still run under
// a plain `go test`. `make test-gateway` guarantees the binaries are present.
var (
	testCfg    *rest.Config
	testScheme *runtime.Scheme
)

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := firstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envtest unavailable, running unit tests only: %v\n", err)
		os.Exit(m.Run())
	}

	testCfg = cfg
	testScheme = runtime.NewScheme()
	mustAddToScheme(clientgoscheme.AddToScheme(testScheme))
	mustAddToScheme(logicv1.AddToScheme(testScheme))

	code := m.Run()
	_ = testEnv.Stop()
	os.Exit(code)
}

func mustAddToScheme(err error) {
	if err != nil {
		panic(err)
	}
}

// firstFoundEnvTestBinaryDir locates the envtest binaries under the repo-root
// bin/k8s directory (mirrors the operator's helper). If KUBEBUILDER_ASSETS is
// set, envtest reads it directly and this returns "".
func firstFoundEnvTestBinaryDir() string {
	if os.Getenv("KUBEBUILDER_ASSETS") != "" {
		return ""
	}
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

// newDefinition builds a Definition carrying the workflow-identity labels and a
// non-trivial flow document (to exercise the spec-stripping transform).
func newDefinition(namespace, name, wfNamespace, wfName, wfVersion string) *logicv1.LogicFlowDefinition {
	return &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				logicv1.LabelWorkflowNamespace: wfNamespace,
				logicv1.LabelWorkflowName:      wfName,
				logicv1.LabelWorkflowVersion:   wfVersion,
			},
		},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: "rt1"},
			Flow:       runtime.RawExtension{Raw: []byte(`{"document":{"dsl":"1.0.0","namespace":"acme","name":"workflow1","version":"v1"},"do":[]}`)},
		},
	}
}

// createServiceWithURL creates a Service that defaults 100% traffic to defName
// and sets its status.URL (status is a subresource, updated separately).
func createServiceWithURL(ctx context.Context, t *testing.T, c client.Client, namespace, name, defName, url string) {
	t.Helper()
	svc := &logicv1.LogicFlowService{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: logicv1.LogicFlowServiceSpec{
			DefaultDefinition: &corev1.LocalObjectReference{Name: defName},
			Ingress:           logicv1.IngressSpec{Host: "host.example.com"},
		},
	}
	require.NoError(t, c.Create(ctx, svc))
	svc.Status.URL = url
	require.NoError(t, c.Status().Update(ctx, svc))
}

// startDiscovery starts a Discovery against the envtest control plane and waits
// for its cache to sync. The caller cancels ctx to stop it.
func startDiscovery(ctx context.Context, t *testing.T) (*Discovery, *RouteStore) {
	t.Helper()
	store := NewRouteStore()
	disc := NewDiscovery(testCfg, store, testScheme)
	go func() { _ = disc.Start(ctx) }()

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.True(t, disc.WaitForCacheSync(syncCtx), "cache should sync")
	return disc, store
}

func newTestClient(t *testing.T) client.Client {
	t.Helper()
	c, err := client.New(testCfg, client.Options{Scheme: testScheme})
	require.NoError(t, err)
	return c
}

func TestEnvtestJoinBuildsRoute(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx := t.Context()

	c := newTestClient(t)
	require.NoError(t, c.Create(ctx, newDefinition("default", "def-a", "acme", "workflow1", "v1")))
	createServiceWithURL(ctx, t, c, "default", "svc-a", "def-a", "https://workflow1.example.com")

	_, store := startDiscovery(ctx, t)

	key := WorkflowKey{Namespace: "acme", Name: "workflow1", Version: "v1"}
	assert.Eventually(t, func() bool {
		_, ok := store.Lookup(key)
		return ok
	}, 10*time.Second, 100*time.Millisecond, "route should appear after cache sync")

	url, ok := store.Lookup(key)
	assert.True(t, ok)
	assert.Equal(t, "https://workflow1.example.com", url)
}

func TestEnvtestReactsToCreateAndDelete(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx := t.Context()

	c := newTestClient(t)
	_, store := startDiscovery(ctx, t)

	// Create a Service+Definition AFTER discovery is running; the watch must pick it up.
	require.NoError(t, c.Create(ctx, newDefinition("default", "def-b", "acme", "orders", "v2")))
	createServiceWithURL(ctx, t, c, "default", "svc-b", "def-b", "https://orders.example.com")

	key := WorkflowKey{Namespace: "acme", Name: "orders", Version: "v2"}
	assert.Eventually(t, func() bool {
		_, ok := store.Lookup(key)
		return ok
	}, 10*time.Second, 100*time.Millisecond, "route should appear after create event")

	// Delete the Service; the route must disappear.
	require.NoError(t, c.Delete(ctx, &logicv1.LogicFlowService{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "svc-b"},
	}))

	assert.Eventually(t, func() bool {
		_, ok := store.Lookup(key)
		return !ok
	}, 10*time.Second, 100*time.Millisecond, "route should disappear after delete event")
}

func TestEnvtestCacheStripsDefinitionSpec(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx := t.Context()

	c := newTestClient(t)
	require.NoError(t, c.Create(ctx, newDefinition("default", "def-c", "acme", "billing", "v1")))
	createServiceWithURL(ctx, t, c, "default", "svc-c", "def-c", "https://billing.example.com")

	disc, _ := startDiscovery(ctx, t)

	// Read the Definition back from Discovery's own cache: the transform must have
	// dropped the flow document while preserving the identity labels.
	var cached logicv1.LogicFlowDefinition
	assert.Eventually(t, func() bool {
		return disc.cache.Get(ctx, client.ObjectKey{Namespace: "default", Name: "def-c"}, &cached) == nil
	}, 10*time.Second, 100*time.Millisecond)

	assert.Empty(t, cached.Spec.Flow.Raw, "flow document must be stripped from the cache")
	assert.Equal(t, "billing", cached.Labels[logicv1.LabelWorkflowName], "labels must survive the transform")
}
