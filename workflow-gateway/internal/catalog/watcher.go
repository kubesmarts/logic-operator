package catalog

import (
	"context"
	"fmt"
	"sync"
	"time"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
	toolscache "k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
)

// debounceInterval coalesces rapid informer events into a single recompute.
const debounceInterval = 500 * time.Millisecond

// Watcher watches LogicFlowDefinitions and LogicFlowServices on a shared cache
// and rebuilds the routing.Resolver's endpoint table on changes. A configured
// LogicFlowService URL takes precedence; the referenced runtime Service is the
// fallback when no matching LogicFlowService route exists.
type Watcher struct {
	cache       crcache.Cache
	resolver    *routing.Resolver
	definitions *definitions.DefinitionStore

	ctx           context.Context
	debounceTimer *time.Timer
	debounceMu    sync.Mutex
	recomputeMu   sync.Mutex

	initialOnce sync.Once
	initialSync chan struct{}
}

func NewWatcher(c crcache.Cache, resolver *routing.Resolver, definitionStore *definitions.DefinitionStore) *Watcher {
	return &Watcher{cache: c, resolver: resolver, definitions: definitionStore, initialSync: make(chan struct{})}
}

// StripDefinitionSpec removes the heavy flow document before the object enters
// the cache, retaining RuntimeRef for routing. Pass this to kube.NewManager's
// ByObject options.
func StripDefinitionSpec(obj any) (any, error) {
	if def, ok := obj.(*logicv1.LogicFlowDefinition); ok {
		def.Spec = logicv1.LogicFlowDefinitionSpec{RuntimeRef: def.Spec.RuntimeRef}
	}
	return obj, nil
}

// Start implements manager.Runnable. The shared cache is started and synced by
// the manager; this registers handlers, does an initial recompute, then blocks.
func (w *Watcher) Start(ctx context.Context) error {
	w.ctx = ctx
	if err := w.setupWatchers(ctx); err != nil {
		return err
	}
	w.doRecompute(ctx)
	w.initialOnce.Do(func() { close(w.initialSync) })
	<-ctx.Done()
	return nil
}

// WaitForInitialSync blocks until the first endpoint recompute has completed, or
// ctx is cancelled. Startup gates readiness on this so the gateway does not serve
// before the routing table is populated.
func (w *Watcher) WaitForInitialSync(ctx context.Context) bool {
	select {
	case <-w.initialSync:
		return true
	case <-ctx.Done():
		return false
	}
}

func (w *Watcher) setupWatchers(ctx context.Context) error {
	eventHandler := toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ any) { w.scheduleRecompute() },
		UpdateFunc: func(_, _ any) { w.scheduleRecompute() },
		DeleteFunc: func(_ any) { w.scheduleRecompute() },
	}
	defInformer, err := w.cache.GetInformer(ctx, &logicv1.LogicFlowDefinition{})
	if err != nil {
		return err
	}
	if _, err := defInformer.AddEventHandler(eventHandler); err != nil {
		return err
	}
	serviceInformer, err := w.cache.GetInformer(ctx, &logicv1.LogicFlowService{})
	if err != nil {
		return err
	}
	if _, err := serviceInformer.AddEventHandler(eventHandler); err != nil {
		return err
	}
	return nil
}

func (w *Watcher) scheduleRecompute() {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.debounceTimer = time.AfterFunc(debounceInterval, func() { w.doRecompute(w.ctx) })
}

func (w *Watcher) doRecompute(ctx context.Context) {
	w.recomputeMu.Lock()
	defer w.recomputeMu.Unlock()
	if w.cache == nil {
		return
	}
	logger := log.FromContext(ctx).WithName("catalog")

	defs := &logicv1.LogicFlowDefinitionList{}
	if err := w.cache.List(ctx, defs); err != nil {
		logger.Error(err, "failed to list LogicFlowDefinitions")
		return
	}
	services := &logicv1.LogicFlowServiceList{}
	if err := w.cache.List(ctx, services); err != nil {
		logger.Error(err, "failed to list LogicFlowServices")
		return
	}

	// Start with direct runtime-service routes so gateway-only workflows work
	// without a LogicFlowService. Overlay configured LogicFlowService URLs for
	// workflows that have one, preserving ingress and traffic-policy behavior.
	endpoints, errs := buildKubernetesServiceEndpoints(defs.Items)
	definitionsByName := make(map[string]*logicv1.LogicFlowDefinition, len(defs.Items))
	for i := range defs.Items {
		def := &defs.Items[i]
		definitionsByName[def.Namespace+"/"+def.Name] = def
	}
	flowServiceEndpoints, serviceErrs := buildLogicFlowServiceEndpoints(services.Items, func(namespace, name string) (*logicv1.LogicFlowDefinition, error) {
		if def, ok := definitionsByName[namespace+"/"+name]; ok {
			return def, nil
		}
		return nil, fmt.Errorf("definition %s/%s not found in gateway cache", namespace, name)
	})
	// Replace kubernetes service endpoints with LogicFlowService endpoints to keep the traffic split policy
	errs = append(errs, serviceErrs...)
	for key, endpoint := range flowServiceEndpoints {
		endpoints[key] = endpoint
	}
	for _, err := range errs {
		logger.Error(err, "error building endpoints")
	}
	w.resolver.UpdateExecutionEndpoints(endpoints)
	definitionKeys := make([]internal.WorkflowKey, 0, len(endpoints))
	for key := range endpoints {
		definitionKeys = append(definitionKeys, key)
	}
	w.definitions.Replace(definitionKeys)
	logger.V(1).Info("workflow endpoint table updated", "numEndpoints", len(endpoints), "numErrors", len(errs))
}
