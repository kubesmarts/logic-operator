package routing

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubesmarts/logic-operator/internal/controller"
)

// ErrWorkflowNotFound is returned when no endpoint matches the workflow identity.
var ErrWorkflowNotFound = errors.New("workflow not found in routing table")

// DestinationKind distinguishes a pod-specific target from a service target.
type DestinationKind int

const (
	DestinationService DestinationKind = iota
	DestinationPod
)

// Destination is the resolved target for an instance operation.
type Destination struct {
	URL  URL
	Kind DestinationKind
}

// PodResolver resolves a lease to the pod currently holding it.
// found is false (with nil error) when the lease is absent or has no holder,
// signalling the caller to fall back to service routing.
type PodResolver interface {
	PodFor(ctx context.Context, namespace, leaseID string) (podName string, found bool, err error)
}

// Resolver resolves workflow and instance requests to upstream URLs.
type Resolver struct {
	endpoints  *ExecutionEndpointStore
	instances  *InstanceStore
	pods       PodResolver
	runnerPort int
}

// Option configures a Resolver.
type Option func(*Resolver)

// WithRunnerPort overrides the port used when building pod URLs. Defaults to
// controller.QuarkusPort; integration tests set it to a mapped container port.
func WithRunnerPort(port int) Option {
	return func(r *Resolver) { r.runnerPort = port }
}

func NewResolver(endpoints *ExecutionEndpointStore, instances *InstanceStore, pods PodResolver, opts ...Option) *Resolver {
	r := &Resolver{
		endpoints:  endpoints,
		instances:  instances,
		pods:       pods,
		runnerPort: int(controller.QuarkusPort),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// UpdateExecutionEndpoints atomically replaces the execution endpoint table.
func (r *Resolver) UpdateExecutionEndpoints(endpoints map[WorkflowKey]URL) {
	r.endpoints.Replace(endpoints)
}

// ExecutionEndpointsSnapshot returns a sorted snapshot of the endpoint table.
func (r *Resolver) ExecutionEndpointsSnapshot() []ExecutionEndpoint {
	return r.endpoints.Snapshot()
}

// ResolveExecution returns the execution endpoint URL for a workflow.
// version may be empty to resolve the latest.
func (r *Resolver) ResolveExecution(namespace, name, version string) (URL, error) {
	if version != "" {
		url, ok := r.endpoints.Lookup(WorkflowKey{Namespace: namespace, Name: name, Version: version})
		if !ok {
			return "", ErrWorkflowNotFound
		}
		return url, nil
	}
	url, ok := r.endpoints.LookupLatest(namespace, name)
	if !ok {
		return "", ErrWorkflowNotFound
	}
	return url, nil
}

// ResolveInstance returns the destination for an instance operation.
// It tries lease->pod routing first, then falls back to service routing.
func (r *Resolver) ResolveInstance(ctx context.Context, namespace, name, instanceID string) (Destination, error) {
	if leaseID, ok := r.instances.Lookup(instanceID); ok {
		if podName, found, err := r.pods.PodFor(ctx, namespace, leaseID); err == nil && found {
			podURL := URL(fmt.Sprintf("http://%s:%d", podName, r.runnerPort))
			return Destination{URL: podURL, Kind: DestinationPod}, nil
		}
	}
	url, err := r.ResolveExecution(namespace, name, "")
	if err != nil {
		return Destination{}, err
	}
	return Destination{URL: url, Kind: DestinationService}, nil
}

// CacheInstance stores the instanceID -> leaseID mapping.
func (r *Resolver) CacheInstance(instanceID, leaseID string) {
	r.instances.Set(instanceID, leaseID)
}

// RemoveInstance deletes an instance from the cache.
func (r *Resolver) RemoveInstance(instanceID string) {
	r.instances.Remove(instanceID)
}
