package routing

import (
	"sort"
	"sync"
)

// ExecutionEndpointStore is a concurrency-safe store of execution endpoints for workflows.
type ExecutionEndpointStore struct {
	mu        sync.RWMutex
	endpoints map[WorkflowKey]URL
}

// NewExecutionEndpointStore creates a new, empty execution endpoint store.
func NewExecutionEndpointStore() *ExecutionEndpointStore {
	return &ExecutionEndpointStore{
		endpoints: make(map[WorkflowKey]URL),
	}
}

// Lookup returns the execution endpoint URL for a workflow key, or ("", false) if not found.
func (s *ExecutionEndpointStore) Lookup(key WorkflowKey) (URL, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	url, ok := s.endpoints[key]
	return url, ok
}

// LookupLatest returns the execution endpoint URL for any version of the workflow identified by
// namespace+name. All versions of a workflow share a single LogicFlowService
// and therefore the same upstream URL; version-based traffic splitting is the
// ingress's responsibility. Returns ("", false) if no endpoint exists.
func (s *ExecutionEndpointStore) LookupLatest(namespace, name string) (URL, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, url := range s.endpoints {
		if key.Namespace == namespace && key.Name == name {
			return url, true
		}
	}
	return "", false
}

// Snapshot returns a copy of the current execution endpoint table as a sorted slice.
// The slice is a copy; mutating it does not affect the store.
func (s *ExecutionEndpointStore) Snapshot() []ExecutionEndpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ExecutionEndpoint, 0, len(s.endpoints))
	for key, url := range s.endpoints {
		result = append(result, ExecutionEndpoint{
			WorkflowKey: key,
			URL:         url,
		})
	}

	// Sort for deterministic output
	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Version < result[j].Version
	})

	return result
}

// Replace atomically swaps the entire endpoint map. The input is copied so that
// the caller cannot bypass the lock by mutating the map after return.
func (s *ExecutionEndpointStore) Replace(endpoints map[WorkflowKey]URL) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Defensive copy: prevent caller from mutating the stored map outside the lock.
	s.endpoints = make(map[WorkflowKey]URL, len(endpoints))
	for k, v := range endpoints {
		s.endpoints[k] = v
	}
}
