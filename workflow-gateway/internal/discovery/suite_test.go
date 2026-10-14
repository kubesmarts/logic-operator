package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/testutil"
)

var (
	testCfg    *rest.Config
	testScheme *runtime.Scheme
)

func TestMain(m *testing.M) {
	var stop func()
	testCfg, testScheme, stop = testutil.StartEnvtest(
		filepath.Join("..", "..", "..", "config", "crd", "bases"),
	)
	code := m.Run()
	stop()
	os.Exit(code)
}

// startDiscovery creates a Discovery against the envtest control plane and
// waits for its cache to sync. The caller cancels ctx to stop it.
func startDiscovery(ctx context.Context, t *testing.T) (*Discovery, *RouteStore) {
	t.Helper()
	store := NewRouteStore()
	c := testutil.NewTestClient(t, testCfg, testScheme)
	disc := NewDiscovery(testCfg, store, testScheme, c)
	go func() { _ = disc.Start(ctx) }()

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.True(t, disc.WaitForCacheSync(syncCtx), "cache should sync")
	return disc, store
}

func TestEnvtestJoinBuildsRoute(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx := t.Context()

	c := testutil.NewTestClient(t, testCfg, testScheme)
	require.NoError(t, c.Create(ctx, testutil.NewDefinition("default", "def-a", "acme", "workflow1", "v1")))
	testutil.CreateServiceWithURL(ctx, t, c, "default", "svc-a", "def-a", "https://workflow1.example.com")

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

	c := testutil.NewTestClient(t, testCfg, testScheme)
	_, store := startDiscovery(ctx, t)

	require.NoError(t, c.Create(ctx, testutil.NewDefinition("default", "def-b", "acme", "orders", "v2")))
	testutil.CreateServiceWithURL(ctx, t, c, "default", "svc-b", "def-b", "https://orders.example.com")

	key := WorkflowKey{Namespace: "acme", Name: "orders", Version: "v2"}
	assert.Eventually(t, func() bool {
		_, ok := store.Lookup(key)
		return ok
	}, 10*time.Second, 100*time.Millisecond, "route should appear after create event")

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

	c := testutil.NewTestClient(t, testCfg, testScheme)
	require.NoError(t, c.Create(ctx, testutil.NewDefinition("default", "def-c", "acme", "billing", "v1")))
	testutil.CreateServiceWithURL(ctx, t, c, "default", "svc-c", "def-c", "https://billing.example.com")

	disc, _ := startDiscovery(ctx, t)

	var cached logicv1.LogicFlowDefinition
	assert.Eventually(t, func() bool {
		return disc.cache.Get(ctx, client.ObjectKey{Namespace: "default", Name: "def-c"}, &cached) == nil
	}, 10*time.Second, 100*time.Millisecond)

	assert.Empty(t, cached.Spec.Flow.Raw, "flow document must be stripped from the cache")
	assert.Equal(t, "billing", cached.Labels[logicv1.LabelWorkflowName], "labels must survive the transform")
}

func TestFetchDefinition(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := testutil.NewTestClient(t, testCfg, testScheme)
	disc, _ := startDiscovery(ctx, t)

	// Create a test definition
	def := testutil.NewDefinition("default", "def-test", "default", "wf", "v1")
	require.NoError(t, c.Create(ctx, def))

	// Fetch it via FetchDefinition
	require.Eventually(t, func() bool {
		flowDef, err := disc.FetchDefinition(ctx, "default", "wf", "v1")
		return err == nil && flowDef != nil
	}, 10*time.Second, 100*time.Millisecond)

	// Verify the returned definition has the Flow document
	flowDef, err := disc.FetchDefinition(ctx, "default", "wf", "v1")
	require.NoError(t, err)
	assert.NotNil(t, flowDef.Spec.Flow)
}
