package v1

import "k8s.io/apimachinery/pkg/types"

// ObjectReference contains enough information to locate a referenced Kubernetes object.
// This is used in status fields to reference related resources (Deployments, Services, DaemonSets, etc.).
//
// Unlike types.NamespacedName, this type has JSON tags and will serialize properly in status fields.
type ObjectReference struct {
	// Name of the referenced object.
	Name string `json:"name"`
	// Namespace of the referenced object.
	// For cluster-scoped resources, this field is empty.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ToNamespacedName converts ObjectReference to types.NamespacedName for use with client.Get() and similar API calls.
func (r ObjectReference) ToNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      r.Name,
		Namespace: r.Namespace,
	}
}

// ObjectReferenceFrom creates an ObjectReference from types.NamespacedName.
// This is useful when populating status fields from API query results.
//
// Example:
//
//	deployment := &appsv1.Application{}
//	_ = r.Get(ctx, types.NamespacedName{Name: "data-index", Namespace: "default"}, deployment)
//	status.DeploymentRef = ObjectReferenceFrom(types.NamespacedName{
//	    Name:      deployment.Name,
//	    Namespace: deployment.Namespace,
//	})
func ObjectReferenceFrom(nn types.NamespacedName) ObjectReference {
	return ObjectReference{
		Name:      nn.Name,
		Namespace: nn.Namespace,
	}
}
