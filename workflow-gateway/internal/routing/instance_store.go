package routing

import (
	"container/list"
	"sync"
)

const DefaultInstanceCacheBytes int64 = 20 << 20

// Estimated per-mapping cost of the map slot, list element, string headers and allocator overhead.
const instanceEntryOverhead int64 = 128

func instanceSize(instanceID, leaseID string) int64 {
	return instanceEntryOverhead + int64(len(instanceID)) + int64(len(leaseID))
}

type instanceMapping struct {
	leaseID  string
	position *list.Element // Value is the instance ID used as the map key.
}

// InstanceStore caches instance-to-lease mappings within an estimated byte budget.
type InstanceStore struct {
	mu        sync.Mutex
	instances map[string]instanceMapping
	recency   list.List
	usedBytes int64
	maxBytes  int64
}

func NewInstanceStore() *InstanceStore {
	return NewInstanceStoreWithLimit(DefaultInstanceCacheBytes)
}

func NewInstanceStoreWithLimit(maxBytes int64) *InstanceStore {
	return &InstanceStore{instances: make(map[string]instanceMapping), maxBytes: maxBytes}
}

func (s *InstanceStore) Lookup(instanceID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mapping, found := s.instances[instanceID]
	if !found {
		return "", false
	}
	s.recency.MoveToFront(mapping.position)
	return mapping.leaseID, true
}

func (s *InstanceStore) Set(instanceID, leaseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	size := instanceSize(instanceID, leaseID)
	if size > s.maxBytes {
		s.remove(instanceID)
		return
	}

	mapping, exists := s.instances[instanceID]
	if exists {
		s.usedBytes -= instanceSize(instanceID, mapping.leaseID)
		s.recency.MoveToFront(mapping.position)
	} else {
		mapping.position = s.recency.PushFront(instanceID)
	}
	for s.usedBytes > s.maxBytes-size {
		s.remove(s.recency.Back().Value.(string))
	}
	mapping.leaseID = leaseID
	s.instances[instanceID] = mapping
	s.usedBytes += size
}

func (s *InstanceStore) Remove(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remove(instanceID)
}

func (s *InstanceStore) remove(instanceID string) {
	if mapping, found := s.instances[instanceID]; found {
		s.recency.Remove(mapping.position)
		delete(s.instances, instanceID)
		s.usedBytes -= instanceSize(instanceID, mapping.leaseID)
	}
}
