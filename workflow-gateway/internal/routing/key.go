package routing

import "github.com/kubesmarts/logic-operator/workflow-gateway/internal"

// URL is a fully-qualified endpoint URL (scheme://host[:port][/path]).
// Every API that returns a URL must use this type, never bare string.
type URL string

// ExecutionEndpoint is a single flattened entry in the execution endpoint table.
type ExecutionEndpoint struct {
	internal.WorkflowKey
	URL URL `json:"url"`
}
