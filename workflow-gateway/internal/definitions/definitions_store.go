package definitions

import (
	"sort"
	"sync"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal"
)

type DefinitionStore struct {
	mu          sync.RWMutex
	definitions []internal.WorkflowKey
}

// NewDefinitionStore creates an empty, concurrency-safe definition catalog.
func NewDefinitionStore() *DefinitionStore {
	return &DefinitionStore{}
}

// Snapshot returns a sorted copy of the workflow identities in the catalog.
func (s *DefinitionStore) Snapshot() []internal.WorkflowKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]internal.WorkflowKey, len(s.definitions))
	copy(result, s.definitions)
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

// Replace atomically replaces the catalog. The input slice is copied so callers
// cannot mutate the stored snapshot after this method returns.
func (s *DefinitionStore) Replace(definitions []internal.WorkflowKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.definitions = append([]internal.WorkflowKey(nil), definitions...)
}
