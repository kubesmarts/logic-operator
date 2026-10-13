package platform

import "sync"

type PlatformStore struct {
	mu       sync.RWMutex
	endpoint string
	set      bool
}

func NewPlatformStore() *PlatformStore { return &PlatformStore{} }

func (s *PlatformStore) SetEndpoint(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoint = url
	s.set = true
}

func (s *PlatformStore) ClearEndpoint() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoint = ""
	s.set = false
}

func (s *PlatformStore) Endpoint() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endpoint, s.set
}
