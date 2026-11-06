package platform

import (
	"context"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	toolscache "k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PlatformWatcher watches ClusterLogicPlatform and keeps PlatformStore updated
// with the current DataIndex GraphQL endpoint. It runs as a controller-runtime
// Runnable on a shared cache.
type PlatformWatcher struct {
	cache     crcache.Cache
	store     *PlatformStore
	namespace string
}

// NewPlatformWatcher creates a watcher bound to a shared cache. namespace is the
// namespace where the Workflow Gateway runs (used for logging; the platform is cluster-scoped).
func NewPlatformWatcher(c crcache.Cache, store *PlatformStore, namespace string) *PlatformWatcher {
	return &PlatformWatcher{cache: c, store: store, namespace: namespace}
}

// Start implements manager.Runnable. It registers event handlers on the shared
// cache's ClusterLogicPlatform informer, performs an initial sync, then blocks until
// ctx is cancelled. The shared cache is started and synced by the manager, so
// this does not create or sync a cache itself.
func (w *PlatformWatcher) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("platform-watcher")

	informer, err := w.cache.GetInformer(ctx, &logicv1.ClusterLogicPlatform{})
	if err != nil {
		return err
	}
	handler := toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { w.syncEndpoint(obj) },
		UpdateFunc: func(_, obj any) { w.syncEndpoint(obj) },
		DeleteFunc: func(_ any) { w.store.ClearEndpoint() },
	}
	if _, err := informer.AddEventHandler(handler); err != nil {
		return err
	}

	w.doSync(ctx)
	logger.Info("platform watcher ready", "namespace", w.namespace)

	<-ctx.Done()
	return nil
}

// ClusterLogicPlatform is cluster-scoped; there is exactly one.
// Data Index runs in the platform's Spec.TargetNamespace.
func (w *PlatformWatcher) syncEndpoint(obj any) {
	p, ok := obj.(*logicv1.ClusterLogicPlatform)
	if !ok {
		return
	}
	if url := dataIndexEndpoint(p); url != "" {
		w.store.SetEndpoint(url)
	} else {
		w.store.ClearEndpoint()
	}
}

func dataIndexEndpoint(p *logicv1.ClusterLogicPlatform) string {
	if p == nil || p.Status.DataIndex == nil {
		return ""
	}
	return p.Status.DataIndex.Service.GraphQLEndpoint
}

func (w *PlatformWatcher) doSync(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("platform-watcher")
	list := &logicv1.ClusterLogicPlatformList{}
	if err := w.cache.List(ctx, list); err != nil {
		logger.Error(err, "failed to list ClusterLogicPlatforms")
		return
	}
	for i := range list.Items {
		if url := dataIndexEndpoint(&list.Items[i]); url != "" {
			w.store.SetEndpoint(url)
			return
		}
	}
	w.store.ClearEndpoint()
}
