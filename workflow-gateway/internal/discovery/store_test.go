package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupHit(t *testing.T) {
	s := NewRouteStore()
	key := WorkflowKey{Namespace: "default", Name: "hello", Version: "1.0"}
	s.Replace(map[WorkflowKey]string{key: "https://hello.example.com"})

	url, ok := s.Lookup(key)
	assert.True(t, ok)
	assert.Equal(t, "https://hello.example.com", url)
}

func TestLookupMiss(t *testing.T) {
	s := NewRouteStore()
	key := WorkflowKey{Namespace: "default", Name: "notfound", Version: "1.0"}

	url, ok := s.Lookup(key)
	assert.False(t, ok)
	assert.Empty(t, url)
}

func TestSnapshot(t *testing.T) {
	s := NewRouteStore()
	m := map[WorkflowKey]string{
		{Namespace: "ns1", Name: "a", Version: "1.0"}: "http://a.example.com",
		{Namespace: "ns1", Name: "b", Version: "2.0"}: "http://b.example.com",
	}
	s.Replace(m)

	snap := s.Snapshot()
	assert.Equal(t, 2, len(snap))
	// Snapshot must be sorted for deterministic output
	assert.Equal(t, "a", snap[0].Name)
	assert.Equal(t, "b", snap[1].Name)
}

func TestSnapshotIsACopy(t *testing.T) {
	s := NewRouteStore()
	s.Replace(map[WorkflowKey]string{
		{Namespace: "ns1", Name: "a", Version: "1.0"}: "http://a.example.com",
	})

	snap1 := s.Snapshot()
	// Mutate the snapshot
	snap1[0].URL = "http://modified.example.com"
	// Original store should be unchanged
	snap2 := s.Snapshot()
	assert.Equal(t, "http://a.example.com", snap2[0].URL)
}

func TestConcurrentLookupAndReplace(_ *testing.T) {
	s := NewRouteStore()
	key := WorkflowKey{Namespace: "ns1", Name: "concurrent", Version: "1.0"}

	// Simulate concurrent access
	go func() {
		for i := 0; i < 100; i++ {
			s.Replace(map[WorkflowKey]string{key: "http://url.example.com"})
		}
	}()

	for i := 0; i < 100; i++ {
		_, _ = s.Lookup(key)
		_ = s.Snapshot()
	}
	// If race detector enabled, this test will catch data races
}
