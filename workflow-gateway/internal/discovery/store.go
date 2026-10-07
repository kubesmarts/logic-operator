package discovery

import (
	"sort"
	"sync"
)

// WorkflowKey identifies a workflow version.
type WorkflowKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

// Route is a single flattened entry in the routing table.
type Route struct {
	WorkflowKey
	URL string `json:"url"`
}

// RouteStore is a concurrency-safe routing table.
type RouteStore struct {
	mu     sync.RWMutex
	routes map[WorkflowKey]string
}

// NewRouteStore creates a new, empty routing store.
func NewRouteStore() *RouteStore {
	return &RouteStore{
		routes: make(map[WorkflowKey]string),
	}
}

// Lookup returns the URL for a workflow key, or ("", false) if not found.
func (s *RouteStore) Lookup(key WorkflowKey) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	url, ok := s.routes[key]
	return url, ok
}

// Snapshot returns a copy of the current routing table as a sorted slice.
// The slice is a copy; mutating it does not affect the store.
func (s *RouteStore) Snapshot() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Route, 0, len(s.routes))
	for key, url := range s.routes {
		result = append(result, Route{
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

// Replace atomically swaps the entire routing map.
func (s *RouteStore) Replace(routes map[WorkflowKey]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = routes
}
