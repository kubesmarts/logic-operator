package discovery

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

// Discovery watches LogicFlowService and LogicFlowDefinition CRDs and maintains a routing table.
type Discovery struct {
	config        *rest.Config
	scheme        *runtime.Scheme
	store         *RouteStore
	cache         crcache.Cache
	syncedMu      sync.Mutex
	synced        bool
	debounceTimer *time.Timer
	debounceMu    sync.Mutex
	recomputeMu   sync.Mutex // Serializes recomputes to prevent stale results from overwriting newer ones
	ctx           context.Context
}

// NewDiscovery creates a new discovery manager.
func NewDiscovery(config *rest.Config, store *RouteStore, scheme *runtime.Scheme) *Discovery {
	return &Discovery{
		config: config,
		scheme: scheme,
		store:  store,
	}
}

// Start builds the informer cache, waits for sync, and populates the routing table.
func (d *Discovery) Start(ctx context.Context) error {
	d.ctx = ctx

	// Configure the cache. ByObject attaches a transform to LogicFlowDefinition so
	// its Spec (the heavy flow document) is zeroed before entering the store. It does
	// not restrict which types are cached — informers are created lazily per type in
	// setupWatchers (GetInformer). LogicFlowService, absent here, is cached with
	// default options (full object), which is what the join needs.
	c, err := crcache.New(d.config, crcache.Options{
		Scheme: d.scheme,
		ByObject: map[client.Object]crcache.ByObject{
			&logicv1.LogicFlowDefinition{}: {Transform: stripDefinitionSpec},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}
	d.cache = c

	if err := d.setupWatchers(ctx); err != nil {
		return fmt.Errorf("failed to set up watchers: %w", err)
	}

	go func() {
		_ = c.Start(ctx)
	}()

	if !c.WaitForCacheSync(ctx) {
		return fmt.Errorf("cache sync failed")
	}

	// Initial recompute once the cache is populated, before marking ready.
	// This ensures WaitForCacheSync() does not return until the routing table
	// is actually populated.
	d.doRecompute(ctx)

	d.syncedMu.Lock()
	d.synced = true
	d.syncedMu.Unlock()

	<-ctx.Done()
	return ctx.Err()
}

// stripDefinitionSpec is a cache transform that drops LogicFlowDefinition.Spec
// (the heavy flow document) before the object enters the informer store. The
// routing join needs only labels and status, never the flow.
func stripDefinitionSpec(obj any) (any, error) {
	if def, ok := obj.(*logicv1.LogicFlowDefinition); ok {
		def.Spec = logicv1.LogicFlowDefinitionSpec{}
	}
	return obj, nil
}

// setupWatchers creates the Service and Definition informers (this is what actually
// starts caching each type) and registers debounced event handlers on them.
func (d *Discovery) setupWatchers(ctx context.Context) error {
	// Creates + caches LogicFlowService with default options (full object).
	svcInformer, err := d.cache.GetInformer(ctx, &logicv1.LogicFlowService{})
	if err != nil {
		return fmt.Errorf("failed to get service informer: %w", err)
	}

	defInformer, err := d.cache.GetInformer(ctx, &logicv1.LogicFlowDefinition{})
	if err != nil {
		return fmt.Errorf("failed to get definition informer: %w", err)
	}

	// Any Service or Definition change coalesces into a single debounced recompute.
	eventHandler := toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ any) { d.scheduleRecompute() },
		UpdateFunc: func(_, _ any) { d.scheduleRecompute() },
		DeleteFunc: func(_ any) { d.scheduleRecompute() },
	}

	if _, err := svcInformer.AddEventHandler(eventHandler); err != nil {
		return fmt.Errorf("failed to add service event handler: %w", err)
	}

	if _, err := defInformer.AddEventHandler(eventHandler); err != nil {
		return fmt.Errorf("failed to add definition event handler: %w", err)
	}

	return nil
}

// debounceInterval coalesces rapid informer events into a single recompute.
const debounceInterval = 500 * time.Millisecond

func (d *Discovery) scheduleRecompute() {
	d.debounceMu.Lock()
	defer d.debounceMu.Unlock()

	if d.debounceTimer != nil {
		d.debounceTimer.Stop()
	}
	d.debounceTimer = time.AfterFunc(debounceInterval, func() {
		d.doRecompute(d.ctx)
	})
}

// doRecompute rebuilds the routing table from cache state. Services drive the
// join; each referenced Definition is resolved from the (spec-stripped) cache.
// Serialized by recomputeMu to prevent overlapping debounce callbacks from
// overwriting newer routes with stale results.
func (d *Discovery) doRecompute(ctx context.Context) {
	d.recomputeMu.Lock()
	defer d.recomputeMu.Unlock()

	if d.cache == nil {
		return
	}

	logger := log.FromContext(ctx).WithName("discovery")

	services := &logicv1.LogicFlowServiceList{}
	if err := d.cache.List(ctx, services); err != nil {
		logger.Error(err, "failed to list LogicFlowServices")
		return
	}

	definitions := &logicv1.LogicFlowDefinitionList{}
	if err := d.cache.List(ctx, definitions); err != nil {
		logger.Error(err, "failed to list LogicFlowDefinitions")
		return
	}

	// Index the cached definitions by namespace/name for O(1) resolution.
	byName := make(map[string]*logicv1.LogicFlowDefinition, len(definitions.Items))
	for i := range definitions.Items {
		def := &definitions.Items[i]
		byName[def.Namespace+"/"+def.Name] = def
	}

	resolve := func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
		if def, ok := byName[namespace+"/"+name]; ok {
			return def, nil
		}
		return nil, fmt.Errorf("definition %s/%s not found in cache", namespace, name)
	}

	routes, errs := buildRoutes(services.Items, resolve)
	for _, err := range errs {
		logger.Error(err, "error building routes")
	}

	d.store.Replace(routes)
	logger.V(1).Info("routing table updated", "numRoutes", len(routes), "numErrors", len(errs))
}

// WaitForCacheSync blocks until the routing cache is synced or ctx is done.
func (d *Discovery) WaitForCacheSync(ctx context.Context) bool {
	syncChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			d.syncedMu.Lock()
			if d.synced {
				d.syncedMu.Unlock()
				close(syncChan)
				return
			}
			d.syncedMu.Unlock()
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()

	select {
	case <-syncChan:
		return true
	case <-ctx.Done():
		return false
	}
}

// GetRouteStore returns the underlying route store (for testing or debugging).
func (d *Discovery) GetRouteStore() *RouteStore {
	return d.store
}
