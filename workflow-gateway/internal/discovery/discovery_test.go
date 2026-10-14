package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewDiscoveryInitializes(t *testing.T) {
	store := NewRouteStore()

	// Should not panic
	disc := NewDiscovery(nil, store, nil, nil)
	assert.NotNil(t, disc)
}

func TestDiscoveryGateReadinessPriorToSync(t *testing.T) {
	store := NewRouteStore()
	disc := NewDiscovery(nil, store, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Before sync, WaitForCacheSync should return false (timeout)
	assert.False(t, disc.WaitForCacheSync(ctx))
}

func TestDiscoveryGetRouteStore(t *testing.T) {
	store := NewRouteStore()
	disc := NewDiscovery(nil, store, nil, nil)

	// GetRouteStore should return the same store
	assert.Same(t, store, disc.GetRouteStore())
}
