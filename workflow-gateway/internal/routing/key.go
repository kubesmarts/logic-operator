package routing

// URL is a fully-qualified endpoint URL (scheme://host[:port][/path]).
// Every API that returns a URL must use this type, never bare string.
type URL string

// WorkflowKey identifies a workflow version.
type WorkflowKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

// ExecutionEndpoint is a single flattened entry in the execution endpoint table.
type ExecutionEndpoint struct {
	WorkflowKey
	URL URL `json:"url"`
}
