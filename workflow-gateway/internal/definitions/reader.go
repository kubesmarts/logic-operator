package definitions

import (
	"context"
	"errors"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrDefinitionNotFound is returned when no matching LogicFlowDefinition exists.
var ErrDefinitionNotFound = errors.New("workflow definition not found")

// Reader fetches full LogicFlowDefinition objects on demand. It uses the shared
// cache to locate a definition by workflow-identity labels, then reads the full
// object (with Spec.Flow) directly from the API server, because the cache stores
// the runtime reference but strips the potentially large flow document.
type Reader struct {
	cache     cache.Cache
	apiReader client.Reader
	store     *DefinitionStore
}

// NewReader returns a new Reader that uses the provided cache and API reader.
func NewReader(c cache.Cache, apiReader client.Reader, store *DefinitionStore) *Reader {
	return &Reader{cache: c, apiReader: apiReader, store: store}
}

// Snapshot returns the watched workflow identities known to the gateway.
func (r *Reader) Snapshot() []internal.WorkflowKey { return r.store.Snapshot() }

// Store returns the catalog store updated by the Kubernetes watcher.
func (r *Reader) Store() *DefinitionStore { return r.store }

// FetchDefinition finds the definition in namespace ns whose labels match the
// workflow name and version, then returns the full object from the API server.
// Here ns/name/version are the WORKFLOW identifiers carried in request labels.
func (r *Reader) FetchDefinition(ctx context.Context, ns, name, version string) (*logicv1.LogicFlowDefinition, error) {
	if r.cache == nil || r.apiReader == nil {
		return nil, ErrDefinitionNotFound
	}
	definitions := &logicv1.LogicFlowDefinitionList{}
	if err := r.cache.List(ctx, definitions, client.InNamespace(ns), client.MatchingLabels{
		logicv1.LabelWorkflowVersion: version,
		logicv1.LabelWorkflowName:    name,
	}); err != nil {
		return nil, err
	}
	if len(definitions.Items) == 0 {
		return nil, ErrDefinitionNotFound
	}
	def := &definitions.Items[0]
	fullDef := &logicv1.LogicFlowDefinition{}
	if err := r.apiReader.Get(ctx, client.ObjectKey{Namespace: def.Namespace, Name: def.Name}, fullDef); err != nil {
		return nil, err
	}
	return fullDef, nil
}
