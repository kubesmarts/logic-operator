package internal

// WorkflowKey identifies a workflow version.
type WorkflowKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}
