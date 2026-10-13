package routing

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

// ErrWorkflowNotFound is returned when no routing table entry matches the
// workflow identity extracted from the request URL.
var ErrWorkflowNotFound = errors.New("workflow not found in routing table")

// Resolve extracts {namespace}, {name}, and optionally {version} from the chi
// route context of r, then looks up the upstream URL in store.
//
// If {version} is absent (empty string — as populated by the version-optional
// chi patterns), it falls back to RouteStore.LookupLatest. This keeps the
// gateway stateless: no data-index query is needed to resolve "latest".
func Resolve(r *http.Request, store *discovery.RouteStore) (string, error) {
	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")

	if version != "" {
		url, ok := store.Lookup(discovery.WorkflowKey{
			Namespace: ns,
			Name:      name,
			Version:   version,
		})
		if !ok {
			return "", ErrWorkflowNotFound
		}
		return url, nil
	}

	url, ok := store.LookupLatest(ns, name)
	if !ok {
		return "", ErrWorkflowNotFound
	}
	return url, nil
}
