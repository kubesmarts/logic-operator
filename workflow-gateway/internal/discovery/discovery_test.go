package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestNewDiscoveryInitializes(t *testing.T) {
	scheme := runtime.NewScheme()
	store := NewRouteStore()

	// Should not panic
	disc := NewDiscovery(nil, store, scheme)
	assert.NotNil(t, disc)
}

func TestDiscoveryGateReadinessPriorToSync(t *testing.T) {
	scheme := runtime.NewScheme()
	store := NewRouteStore()
	disc := NewDiscovery(nil, store, scheme)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Before sync, WaitForCacheSync should return false (timeout)
	assert.False(t, disc.WaitForCacheSync(ctx))
}

func TestDiscoveryGetRouteStore(t *testing.T) {
	scheme := runtime.NewScheme()
	store := NewRouteStore()
	disc := NewDiscovery(nil, store, scheme)

	// GetRouteStore should return the same store
	assert.Same(t, store, disc.GetRouteStore())
}
