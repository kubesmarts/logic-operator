package lease

import (
	"context"
	"fmt"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
)

// Resolver resolves a workflow instance's lease to the pod holding it.
// It implements routing.PodResolver.
type Resolver struct {
	client client.Client
}

// compile-time check that Resolver satisfies the port.
var _ routing.PodResolver = (*Resolver)(nil)

func NewResolver(c client.Client) *Resolver {
	return &Resolver{client: c}
}

// PodFor returns the holder identity of the named lease in namespace.
// Returns ("", false, nil) when the lease is absent or has no holder, which
// signals the caller to fall back to service routing. Returns an error only on
// unexpected Kubernetes API failures.
func (r *Resolver) PodFor(ctx context.Context, namespace, leaseID string) (string, bool, error) {
	lease := &coordinationv1.Lease{}
	key := types.NamespacedName{Namespace: namespace, Name: leaseID}
	if err := r.client.Get(ctx, key, lease); err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to get lease %s/%s: %w", namespace, leaseID, err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return "", false, nil
	}
	return *lease.Spec.HolderIdentity, true, nil
}
