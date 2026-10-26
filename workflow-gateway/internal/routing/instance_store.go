package routing

import "sync"

// InstanceStore is a concurrency-safe cache of instanceID -> leaseID mappings.
// leaseID is in the format "namespace/leaseName".
type InstanceStore struct {
	mu        sync.RWMutex
	instances map[string]string
}

func NewInstanceStore() *InstanceStore {
	return &InstanceStore{instances: make(map[string]string)}
}

func (s *InstanceStore) Lookup(instanceID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	leaseID, ok := s.instances[instanceID]
	return leaseID, ok
}

func (s *InstanceStore) Set(instanceID, leaseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[instanceID] = leaseID
}

func (s *InstanceStore) Remove(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.instances, instanceID)
}
