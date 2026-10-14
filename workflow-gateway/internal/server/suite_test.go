package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/dataindex"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/platform"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
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

// startDiscovery starts a Discovery against the envtest control plane and waits
// for its cache to sync. The caller cancels ctx to stop the watcher goroutine.
func startDiscovery(ctx context.Context, t *testing.T, store *discovery.RouteStore) *discovery.Discovery {
	t.Helper()
	c := testutil.NewTestClient(t, testCfg, testScheme)
	disc := discovery.NewDiscovery(testCfg, store, testScheme, c)
	go func() { _ = disc.Start(ctx) }()

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.True(t, disc.WaitForCacheSync(syncCtx), "discovery cache must sync within 30s")
	return disc
}

// startPlatformWatcher starts a PlatformWatcher against the envtest control plane and waits
// for its cache to sync. The caller cancels ctx to stop the watcher goroutine.
func startPlatformWatcher(ctx context.Context, t *testing.T, store *platform.PlatformStore, namespace string) {
	t.Helper()
	watcher := platform.NewPlatformWatcher(testCfg, testScheme, store, namespace)
	go func() { _ = watcher.Start(ctx) }()

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.True(t, watcher.WaitForCacheSync(syncCtx), "platform watcher cache must sync within 30s")
}

// mockDataIndexServer returns an httptest.Server that validates GraphQL queries and returns test instances.
func mockDataIndexServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		// Validate query structure
		assert.NotEmpty(t, req.Query, "query must not be empty")
		assert.NotNil(t, req.Variables, "variables must be present (not null)")

		// Return different responses for list vs get queries
		w.Header().Set("Content-Type", "application/json")
		if req.Variables != nil {
			if _, hasFilter := req.Variables["filter"]; hasFilter {
				// ListInstances query
				resp := map[string]any{
					"data": map[string]any{
						"getWorkflowInstances": []map[string]any{
							{
								"id":        "instance-1",
								"namespace": "test-ns",
								"name":      "test-workflow",
								"version":   "v1",
								"status":    "RUNNING",
								"startedAt": "2026-10-13T10:00:00Z",
								"endedAt":   nil,
							},
							{
								"id":        "instance-2",
								"namespace": "test-ns",
								"name":      "test-workflow",
								"version":   "v1",
								"status":    "COMPLETED",
								"startedAt": "2026-10-13T09:00:00Z",
								"endedAt":   "2026-10-13T09:30:00Z",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			} else if _, hasID := req.Variables["id"]; hasID {
				// GetInstance query
				resp := map[string]any{
					"data": map[string]any{
						"getWorkflowInstance": map[string]any{
							"id":        "instance-1",
							"namespace": "test-ns",
							"name":      "test-workflow",
							"version":   "v1",
							"status":    "RUNNING",
							"startedAt": "2026-10-13T10:00:00Z",
							"endedAt":   nil,
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			}
		}
	}))
}

// TestE2EListDefinitions verifies the end-to-end path from CRD creation in the
// envtest cluster, through the informer cache and RouteStore, to the HTTP
// response from the server's GET /v1/definitions endpoint.
func TestE2EListDefinitions(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := testutil.NewTestClient(t, testCfg, testScheme)
	require.NoError(t, c.Create(ctx, testutil.NewDefinition(
		"default", "def-hello",
		"acme", "hello", "v1",
	)))
	testutil.CreateServiceWithURL(ctx, t, c,
		"default", "svc-hello", "def-hello",
		"https://hello.example.com",
	)

	store := discovery.NewRouteStore()
	disc := startDiscovery(ctx, t, store)

	key := discovery.WorkflowKey{Namespace: "acme", Name: "hello", Version: "v1"}
	require.Eventually(t, func() bool {
		_, ok := store.Lookup(key)
		return ok
	}, 10*time.Second, 100*time.Millisecond, "route must appear in store before querying server")

	srv := server.New(server.DefaultConfig(), disc, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/definitions")
	require.NoError(t, err)
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var keys []discovery.WorkflowKey
	require.NoError(t, json.Unmarshal(body, &keys))
	require.Len(t, keys, 1)
	assert.Equal(t, "acme", keys[0].Namespace)
	assert.Equal(t, "hello", keys[0].Name)
	assert.Equal(t, "v1", keys[0].Version)

	// Upstream URL must never be exposed to API clients.
	assert.NotContains(t, string(body), "hello.example.com")
}

// TestE2EDataIndexDiscoveryAndProxy verifies the end-to-end Data Index integration:
// LogicPlatform CRD creation → PlatformWatcher sync → endpoint discovery → GraphQL queries.
func TestE2EDataIndexDiscoveryAndProxy(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Start mock Data Index GraphQL server
	diServer := mockDataIndexServer(t)
	defer diServer.Close()

	// Start PlatformWatcher first
	platformStore := platform.NewPlatformStore()
	c := testutil.NewTestClient(t, testCfg, testScheme)
	startPlatformWatcher(ctx, t, platformStore, "default")

	// Now create LogicPlatform — watcher's event handlers will discover it
	lp := &logicv1.LogicPlatform{
		ObjectMeta: v1.ObjectMeta{Namespace: "default", Name: "test-platform"},
		Spec:       logicv1.LogicPlatformSpec{},
	}
	require.NoError(t, c.Create(ctx, lp))

	// Update status with endpoint — triggers watcher's UpdateFunc
	lp.Status.DataIndex.Service.GraphQLEndpoint = diServer.URL + "/graphql"
	require.NoError(t, c.Status().Update(ctx, lp))

	// Verify endpoint is discovered via event-driven sync
	require.Eventually(t, func() bool {
		url, ok := platformStore.Endpoint()
		return ok && url == diServer.URL+"/graphql"
	}, 10*time.Second, 100*time.Millisecond, "endpoint must be discovered via watcher's event handler")

	// Create server with Data Index client
	diClient := dataindex.NewClient(platformStore)
	routeStore := discovery.NewRouteStore()
	kubeClient := testutil.NewTestClient(t, testCfg, testScheme)
	disc := discovery.NewDiscovery(testCfg, routeStore, testScheme, kubeClient)
	srv := server.New(server.DefaultConfig(), disc, diClient)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Test ListInstances handler
	t.Run("ListInstances", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/instances")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		var instances []dataindex.WorkflowInstance
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&instances))
		require.Len(t, instances, 2)
		assert.Equal(t, "instance-1", instances[0].ID)
		assert.Equal(t, "RUNNING", instances[0].Status)
		assert.Equal(t, "instance-2", instances[1].ID)
		assert.Equal(t, "COMPLETED", instances[1].Status)
	})

	// Test GetInstance handler
	t.Run("GetInstance", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/v1/instances/instance-1")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		var instance dataindex.WorkflowInstance
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&instance))
		assert.Equal(t, "instance-1", instance.ID)
		assert.Equal(t, "test-ns", instance.Namespace)
		assert.Equal(t, "test-workflow", instance.Name)
		assert.Equal(t, "v1", instance.Version)
		assert.Equal(t, "RUNNING", instance.Status)
	})
}

// TestE2EGetDefinition verifies the end-to-end path for fetching a workflow definition
// from the cache through the handler and returning it as JSON.
func TestE2EGetDefinition(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable; run 'make test-gateway'")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := testutil.NewTestClient(t, testCfg, testScheme)

	// Create definition
	def := testutil.NewDefinition("default", "def-e2e", "default", "hello", "v1")
	require.NoError(t, c.Create(ctx, def))

	// Create service to route it
	testutil.CreateServiceWithURL(ctx, t, c, "default", "svc-e2e", "def-e2e", "https://hello.example.com")

	// Start discovery
	store := discovery.NewRouteStore()
	kubeClient := testutil.NewTestClient(t, testCfg, testScheme)
	disc := discovery.NewDiscovery(testCfg, store, testScheme, kubeClient)
	go func() { _ = disc.Start(ctx) }()

	// Wait for cache sync
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	require.True(t, disc.WaitForCacheSync(syncCtx), "discovery must sync")
	cancel()

	// Create server and test handler
	diClient := dataindex.NewClient(platform.NewPlatformStore())
	srv := server.New(server.DefaultConfig(), disc, diClient)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Query the definition
	resp, err := http.Get(ts.URL + "/v1/definitions/default/hello/v1")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var flowDoc any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&flowDoc))
	assert.NotNil(t, flowDoc)
}
