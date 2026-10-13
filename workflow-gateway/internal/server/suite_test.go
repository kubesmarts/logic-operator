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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
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
func startDiscovery(ctx context.Context, t *testing.T, store *discovery.RouteStore) {
	t.Helper()
	disc := discovery.NewDiscovery(testCfg, store, testScheme)
	go func() { _ = disc.Start(ctx) }()

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.True(t, disc.WaitForCacheSync(syncCtx), "discovery cache must sync within 30s")
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
	startDiscovery(ctx, t, store)

	key := discovery.WorkflowKey{Namespace: "acme", Name: "hello", Version: "v1"}
	require.Eventually(t, func() bool {
		_, ok := store.Lookup(key)
		return ok
	}, 10*time.Second, 100*time.Millisecond, "route must appear in store before querying server")

	srv := server.New(server.DefaultConfig(), store, nil)
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
