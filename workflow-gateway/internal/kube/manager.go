package kube

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Manager wraps a controller-runtime manager, providing a single shared cache,
// client, and runnable lifecycle for all gateway watchers.
type Manager struct {
	mgr manager.Manager
}

// NewManager builds a shared manager. byObject sets per-type cache options
// (e.g. a transform to strip heavy fields before an object enters the cache).
func NewManager(cfg *rest.Config, scheme *runtime.Scheme, byObject map[client.Object]cache.ByObject) (*Manager, error) {
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Metrics:        metricsserver.Options{BindAddress: "0"},
		Cache:          cache.Options{ByObject: byObject},
	})
	if err != nil {
		return nil, err
	}
	return &Manager{mgr: mgr}, nil
}

// GetCache returns the shared informer cache.
func (m *Manager) GetCache() cache.Cache { return m.mgr.GetCache() }

// GetClient returns the cache-backed client (reads come from the cache).
func (m *Manager) GetClient() client.Client { return m.mgr.GetClient() }

// GetAPIReader returns a direct, uncached reader (for full objects that the
// cache may have stripped via a transform).
func (m *Manager) GetAPIReader() client.Reader { return m.mgr.GetAPIReader() }

// Add registers a runnable (e.g. a watcher) with the manager.
func (m *Manager) Add(r manager.Runnable) error { return m.mgr.Add(r) }

// Start starts the cache and all runnables, blocking until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error { return m.mgr.Start(ctx) }

// WaitForCacheSync blocks until the shared cache has synced.
func (m *Manager) WaitForCacheSync(ctx context.Context) bool {
	return m.mgr.GetCache().WaitForCacheSync(ctx)
}
