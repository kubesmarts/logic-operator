package platform

import (
	"context"
	"fmt"
	"sync"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type PlatformWatcher struct {
	config    *rest.Config
	scheme    *runtime.Scheme
	store     *PlatformStore
	namespace string
	cache     crcache.Cache
	ctx       context.Context
	syncedMu  sync.Mutex
	synced    bool
}

func NewPlatformWatcher(config *rest.Config, scheme *runtime.Scheme, store *PlatformStore, namespace string) *PlatformWatcher {
	return &PlatformWatcher{config: config, scheme: scheme, store: store, namespace: namespace}
}

func (w *PlatformWatcher) Start(ctx context.Context) error {
	w.ctx = ctx
	logger := log.FromContext(ctx).WithName("platform-watcher")

	c, err := crcache.New(w.config, crcache.Options{
		Scheme:            w.scheme,
		DefaultNamespaces: map[string]crcache.Config{w.namespace: {}},
	})
	if err != nil {
		return fmt.Errorf("failed to create platform cache: %w", err)
	}
	w.cache = c

	informer, err := c.GetInformer(ctx, &logicv1.LogicPlatform{})
	if err != nil {
		return fmt.Errorf("failed to get LogicPlatform informer: %w", err)
	}

	handler := toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { w.syncEndpoint(obj) },
		UpdateFunc: func(_, obj any) { w.syncEndpoint(obj) },
		DeleteFunc: func(_ any) { w.store.ClearEndpoint() },
	}
	if _, err := informer.AddEventHandler(handler); err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	go func() { _ = c.Start(ctx) }()

	if !c.WaitForCacheSync(ctx) {
		return fmt.Errorf("platform cache sync failed")
	}

	// Initial sync from cache before marking ready
	w.doSync(ctx)

	w.syncedMu.Lock()
	w.synced = true
	w.syncedMu.Unlock()

	logger.Info("platform watcher ready", "namespace", w.namespace)
	<-ctx.Done()
	return ctx.Err()
}

func (w *PlatformWatcher) syncEndpoint(obj any) {
	p, ok := obj.(*logicv1.LogicPlatform)
	if !ok {
		return
	}
	url := p.Status.DataIndex.Service.GraphQLEndpoint
	if url != "" {
		w.store.SetEndpoint(url)
	} else {
		w.store.ClearEndpoint()
	}
}

func (w *PlatformWatcher) doSync(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("platform-watcher")
	list := &logicv1.LogicPlatformList{}
	if err := w.cache.List(ctx, list, client.InNamespace(w.namespace)); err != nil {
		logger.Error(err, "failed to list LogicPlatforms")
		return
	}
	for i := range list.Items {
		if url := list.Items[i].Status.DataIndex.Service.GraphQLEndpoint; url != "" {
			w.store.SetEndpoint(url)
			return
		}
	}
	w.store.ClearEndpoint()
}

func (w *PlatformWatcher) WaitForCacheSync(ctx context.Context) bool {
	ch := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			w.syncedMu.Lock()
			if w.synced {
				w.syncedMu.Unlock()
				close(ch)
				return
			}
			w.syncedMu.Unlock()
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}
