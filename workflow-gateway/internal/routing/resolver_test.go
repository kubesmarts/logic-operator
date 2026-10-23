package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePodResolver is a test implementation of PodResolver.
type fakePodResolver struct {
	pods map[string]string // leaseID -> podURL
}

func newFakePodResolver(pods map[string]string) *fakePodResolver {
	return &fakePodResolver{pods: pods}
}

func (f *fakePodResolver) PodFor(_ context.Context, _, leaseID string) (string, bool, error) {
	if pod, ok := f.pods[leaseID]; ok {
		return pod, true, nil
	}
	return "", false, nil
}

func TestResolveExecution_WithVersion_Hit(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	key := WorkflowKey{Namespace: "default", Name: "hello", Version: "1.0"}
	endpoints.Replace(map[WorkflowKey]URL{key: "https://hello.example.com"})

	url, err := resolver.ResolveExecution("default", "hello", "1.0")
	require.NoError(t, err)
	assert.Equal(t, URL("https://hello.example.com"), url)
}

func TestResolveExecution_WithVersion_Miss(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	url, err := resolver.ResolveExecution("default", "hello", "1.0")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowNotFound))
	assert.Empty(t, url)
}

func TestResolveExecution_Latest_Hit(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	key := WorkflowKey{Namespace: "default", Name: "hello", Version: "2.0"}
	endpoints.Replace(map[WorkflowKey]URL{key: "https://hello.example.com"})

	url, err := resolver.ResolveExecution("default", "hello", "")
	require.NoError(t, err)
	assert.Equal(t, URL("https://hello.example.com"), url)
}

func TestResolveExecution_Latest_Miss(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	url, err := resolver.ResolveExecution("default", "missing", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowNotFound))
	assert.Empty(t, url)
}

func TestResolveInstance_CachedAndPodFound(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(map[string]string{"default/my-lease": "pod-abc123"})
	resolver := NewResolver(endpoints, instances, pods)

	// Cache the instance
	resolver.CacheInstance("instance-1", "default/my-lease")

	dest, err := resolver.ResolveInstance(context.Background(), "default", "hello", "instance-1")
	require.NoError(t, err)
	assert.Equal(t, URL("http://pod-abc123:8080"), dest.URL)
	assert.Equal(t, DestinationPod, dest.Kind)
}

func TestResolveInstance_NotCached_FallsBackToService(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	key := WorkflowKey{Namespace: "default", Name: "hello", Version: "1.0"}
	endpoints.Replace(map[WorkflowKey]URL{key: "https://hello.example.com"})

	dest, err := resolver.ResolveInstance(context.Background(), "default", "hello", "unknown-instance")
	require.NoError(t, err)
	assert.Equal(t, URL("https://hello.example.com"), dest.URL)
	assert.Equal(t, DestinationService, dest.Kind)
}

func TestResolveInstance_PodLookupReturnsNotFound_FallsBackToService(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(map[string]string{}) // No pods; all lookups return found=false
	resolver := NewResolver(endpoints, instances, pods)

	key := WorkflowKey{Namespace: "default", Name: "hello", Version: "1.0"}
	endpoints.Replace(map[WorkflowKey]URL{key: "https://hello.example.com"})

	// Cache the instance with a lease, but the pod won't be found
	resolver.CacheInstance("instance-1", "default/my-lease")

	dest, err := resolver.ResolveInstance(context.Background(), "default", "hello", "instance-1")
	require.NoError(t, err)
	assert.Equal(t, URL("https://hello.example.com"), dest.URL)
	assert.Equal(t, DestinationService, dest.Kind)
}

func TestResolveInstance_NoServiceEndpoint_ReturnsError(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	dest, err := resolver.ResolveInstance(context.Background(), "default", "missing", "unknown-instance")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowNotFound))
	assert.Empty(t, dest.URL)
}

func TestCacheAndRemoveInstance(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	resolver.CacheInstance("instance-1", "default/lease-1")
	leaseID, ok := instances.Lookup("instance-1")
	require.True(t, ok)
	assert.Equal(t, "default/lease-1", leaseID)

	resolver.RemoveInstance("instance-1")
	_, ok = instances.Lookup("instance-1")
	assert.False(t, ok)
}

func TestUpdateAndSnapshotExecutionEndpoints(t *testing.T) {
	endpoints := NewExecutionEndpointStore()
	instances := NewInstanceStore()
	pods := newFakePodResolver(nil)
	resolver := NewResolver(endpoints, instances, pods)

	endpointMap := map[WorkflowKey]URL{
		{Namespace: "ns1", Name: "a", Version: "1.0"}: "http://a.example.com",
		{Namespace: "ns1", Name: "b", Version: "2.0"}: "http://b.example.com",
	}
	resolver.UpdateExecutionEndpoints(endpointMap)

	snap := resolver.ExecutionEndpointsSnapshot()
	require.Equal(t, 2, len(snap))
	// Snapshot is sorted
	assert.Equal(t, "a", snap[0].Name)
	assert.Equal(t, "b", snap[1].Name)
}
