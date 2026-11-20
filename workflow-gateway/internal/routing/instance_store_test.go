package routing

import (
	"strings"
	"testing"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal"
	"github.com/stretchr/testify/require"
)

func TestInstanceCacheEvictionFallsBackToService(t *testing.T) {
	size := instanceSize("a", "lease")
	store := NewInstanceStoreWithLimit(2 * size)
	resolver := NewResolver(NewExecutionEndpointStore(), store, newFakePodResolver(map[string]string{"lease": "pod"}))
	resolver.UpdateExecutionEndpoints(map[internal.WorkflowKey]URL{
		{Namespace: "ns", Name: "flow", Version: "1"}: "http://service",
	})
	store.Set("a", "lease")
	store.Set("b", "lease")
	_, found := store.Lookup("a")
	require.True(t, found)
	store.Set("c", "lease")
	dest, err := resolver.ResolveInstance(t.Context(), "ns", "flow", "a")
	require.NoError(t, err)
	require.Equal(t, DestinationPod, dest.Kind)
	dest, err = resolver.ResolveInstance(t.Context(), "ns", "flow", "b")
	require.NoError(t, err)
	require.Equal(t, Destination{URL: "http://service", Kind: DestinationService}, dest)
}

func TestInstanceCacheByteAccounting(t *testing.T) {
	size := instanceSize("a", "lease")
	store := NewInstanceStoreWithLimit(3 * size)
	for _, id := range []string{"a", "b", "c"} {
		store.Set(id, "lease")
	}
	// Growing an existing entry to the exact budget evicts both other entries.
	large := strings.Repeat("x", int(3*size-instanceEntryOverhead-1))
	store.Set("a", large)
	require.Len(t, store.instances, 1)
	require.Equal(t, 3*size, store.usedBytes)
	store.Set("a", "lease")
	store.Set("b", "lease")
	// An oversized replacement invalidates the old mapping and preserves peers.
	store.Set("a", large+"x")
	_, found := store.Lookup("a")
	require.False(t, found)
	lease, found := store.Lookup("b")
	require.True(t, found)
	require.Equal(t, "lease", lease)
	require.Equal(t, size, store.usedBytes)
	store.Remove("b")
	store.Remove("b")
	require.Zero(t, store.usedBytes)
	require.Empty(t, store.instances)
	require.Zero(t, store.recency.Len())
}
