package routing

import (
	"context"
	"errors"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
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
	endpoints        *ExecutionEndpointStore
	runtimeEndpoints *ExecutionEndpointStore
	instances        *InstanceStore
	pods             PodResolver
	runnerPort       int
}

// Option configures a Resolver.
type Option func(*Resolver)

// WithRunnerPort overrides the shared Quarkus Flow runner HTTP port used when
// building pod URLs. Integration tests set it to a mapped container port.
func WithRunnerPort(port int) Option {
	return func(r *Resolver) { r.runnerPort = port }
}

func NewResolver(endpoints *ExecutionEndpointStore, instances *InstanceStore, pods PodResolver, opts ...Option) *Resolver {
	r := &Resolver{
		endpoints:        endpoints,
		runtimeEndpoints: NewExecutionEndpointStore(),
		instances:        instances,
		pods:             pods,
		runnerPort:       int(logicv1.QuarkusFlowRunnerHTTPPort),
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

// UpdateRuntimeEndpoints replaces the direct Kubernetes Service URLs used for
// runner discovery. These remain separate from execution endpoints, which may
// be LogicFlowService ingress URLs.
func (r *Resolver) UpdateRuntimeEndpoints(endpoints map[WorkflowKey]URL) {
	r.runtimeEndpoints.Replace(endpoints)
}

// DiscoveryCandidates returns direct runtime-Service endpoints for a workflow.
// A supplied version selects one endpoint. Without a version, all registered
// versions are returned in descending lexical order for deterministic probing.
func (r *Resolver) DiscoveryCandidates(namespace, name, version string) []ExecutionEndpoint {
	var candidates []ExecutionEndpoint
	for _, endpoint := range r.runtimeEndpoints.Snapshot() {
		if endpoint.Namespace != namespace || endpoint.Name != name {
			continue
		}
		if version != "" && endpoint.Version != version {
			continue
		}
		candidates = append(candidates, endpoint)
	}
	if version == "" {
		for left, right := 0, len(candidates)-1; left < right; left, right = left+1, right-1 {
			candidates[left], candidates[right] = candidates[right], candidates[left]
		}
	}
	return candidates
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
func (r *Resolver) ResolveInstance(ctx context.Context, namespace, name, version, instanceID string) (Destination, error) {
	if leaseID, ok := r.instances.Lookup(instanceID); ok && r.pods != nil {
		if podName, found, err := r.pods.PodFor(ctx, namespace, leaseID); err == nil && found {
			podURL := URL(fmt.Sprintf("http://%s:%d", podName, r.runnerPort))
			return Destination{URL: podURL, Kind: DestinationPod}, nil
		}
		// The lease may have expired, moved, or be temporarily unavailable.
		// Drop the stale mapping so the next operation attempts discovery again.
		r.instances.Remove(instanceID)
	}
	url, err := r.ResolveExecution(namespace, name, version)
	if err != nil {
		return Destination{}, err
	}
	return Destination{URL: url, Kind: DestinationService}, nil
}

// CacheInstance stores the instanceID -> leaseID mapping.
func (r *Resolver) CacheInstance(instanceID, leaseID string) {
	r.instances.Set(instanceID, leaseID)
}

// HasCachedInstance reports whether the gateway already knows the instance's
// lease identity.
func (r *Resolver) HasCachedInstance(instanceID string) bool {
	_, ok := r.instances.Lookup(instanceID)
	return ok
}

// RemoveInstance deletes an instance from the cache.
func (r *Resolver) RemoveInstance(instanceID string) {
	r.instances.Remove(instanceID)
}
