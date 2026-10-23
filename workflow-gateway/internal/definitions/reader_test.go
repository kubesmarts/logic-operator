package definitions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/kube"
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

// stripSpec is a local transform that zeros the Spec of LogicFlowDefinition
// objects before they enter the cache, simulating what discovery does.
func stripSpec(obj any) (any, error) {
	if d, ok := obj.(*logicv1.LogicFlowDefinition); ok {
		d.Spec = logicv1.LogicFlowDefinitionSpec{}
	}
	return obj, nil
}

// buildReader creates a shared manager with the stripSpec transform and returns
// a Reader configured with the cache and API reader.
func buildReader(ctx context.Context, t *testing.T) *Reader {
	t.Helper()
	mgr, err := kube.NewManager(testCfg, testScheme, map[client.Object]cache.ByObject{
		&logicv1.LogicFlowDefinition{}: {Transform: stripSpec},
	})
	require.NoError(t, err)

	go func() { _ = mgr.Start(ctx) }()
	require.True(t, mgr.WaitForCacheSync(ctx))

	return NewReader(mgr.GetCache(), mgr.GetAPIReader())
}

func TestFetchDefinitionReturnsFullSpec(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := buildReader(ctx, t)
	c := testutil.NewTestClient(t, testCfg, testScheme)

	// Create a test definition
	def := testutil.NewDefinition("default", "def-test", "default", "wf", "v1")
	require.NoError(t, c.Create(ctx, def))

	// Fetch it via FetchDefinition
	require.Eventually(t, func() bool {
		flowDef, err := reader.FetchDefinition(ctx, "default", "wf", "v1")
		return err == nil && flowDef != nil
	}, 10*time.Second, 100*time.Millisecond)

	// Verify the returned definition has the Flow document
	flowDef, err := reader.FetchDefinition(ctx, "default", "wf", "v1")
	require.NoError(t, err)
	assert.NotNil(t, flowDef.Spec.Flow)
	assert.Equal(t, "wf", flowDef.Labels[logicv1.LabelWorkflowName])
	assert.Equal(t, "v1", flowDef.Labels[logicv1.LabelWorkflowVersion])
}

func TestFetchDefinitionNotFound(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := buildReader(ctx, t)

	// Try to fetch a definition that doesn't exist
	_, err := reader.FetchDefinition(ctx, "default", "nonexistent", "v1")
	assert.ErrorIs(t, err, ErrDefinitionNotFound)
}

func TestFetchDefinitionWrongNamespaceOrVersion(t *testing.T) {
	if testCfg == nil {
		t.Skip("envtest unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := buildReader(ctx, t)
	c := testutil.NewTestClient(t, testCfg, testScheme)

	// Create a test definition
	def := testutil.NewDefinition("default", "def-test-2", "default", "wf", "v1")
	require.NoError(t, c.Create(ctx, def))

	// Wait for cache to sync
	time.Sleep(500 * time.Millisecond)

	// Try to fetch with wrong version
	_, err := reader.FetchDefinition(ctx, "default", "wf", "v2")
	assert.ErrorIs(t, err, ErrDefinitionNotFound)

	// Try to fetch with wrong name
	_, err = reader.FetchDefinition(ctx, "default", "different", "v1")
	assert.ErrorIs(t, err, ErrDefinitionNotFound)
}
