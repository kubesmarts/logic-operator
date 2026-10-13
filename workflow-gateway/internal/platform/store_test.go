package platform_test

import (
	"testing"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/platform"
)

func TestPlatformStore_EndpointLifecycle(t *testing.T) {
	s := platform.NewPlatformStore()

	if _, ok := s.Endpoint(); ok {
		t.Fatal("expected empty initially")
	}

	s.SetEndpoint("http://data-index.default.svc.cluster.local/graphql")
	got, ok := s.Endpoint()
	if !ok || got != "http://data-index.default.svc.cluster.local/graphql" {
		t.Fatalf("expected endpoint, got %q %v", got, ok)
	}

	s.ClearEndpoint()
	if _, ok := s.Endpoint(); ok {
		t.Fatal("expected empty after clear")
	}
}

func TestPlatformStore_ConcurrentAccess(_ *testing.T) {
	s := platform.NewPlatformStore()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.SetEndpoint("http://x")
			s.ClearEndpoint()
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		s.Endpoint()
	}
	<-done
}
