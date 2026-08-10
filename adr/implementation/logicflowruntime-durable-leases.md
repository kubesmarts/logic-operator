# LogicFlowRuntime Durable Lease Management Design

**Date:** 2026-08-07
**Status:** Accepted

## Context

The Quarkus Flow `durable-kubernetes` extension uses Kubernetes Leases for multi-pod workflow sharding. Each pod acquires a member lease, and the lease name becomes the `WorkflowApplication` ID — enabling per-pod workflow instance ownership.

In the default quarkus-flow setup, a leader-elected pod creates member leases by walking the Pod→ReplicaSet→Deployment chain to discover `spec.replicas`. This requires the runtime pods to have broad RBAC (leases CRUD, pods/deployments/replicasets read) and introduces startup latency from leader election.

The operator already knows the replica count and owns the Deployment. By pre-creating leases and disabling the leader controller, we eliminate leader election overhead, reduce pod RBAC surface, and guarantee leases exist before pods start.

## Scope

This design covers:

- Lease lifecycle management (create/delete) in the Runtime controller
- Durable env vars on the container
- Pod RBAC (ServiceAccount, ClusterRole, RoleBinding) for lease acquisition
- Status updates (LeaseReady condition, LeaseReplicas field)
- Operator RBAC for leases

## Activation Rule

Lease management activates when `rt.Spec.Persistence != nil`. This is the correct gate because:

- The `standard` and `messaging` runner images include the durable-kubernetes extension; the `minimal` image does not
- The operator already selects the standard image when persistence is configured
- The durable extension's readiness probe gates on lease acquisition (`health.readiness.require-lease=true`), so pods cannot become ready without leases
- Leases are always created, even for replicas=1

## Reconciliation Flow

Updated reconcile order:

```
1. Fetch LogicFlowRuntime (IgnoreNotFound)
2. List ConfigMaps with label runtime-ref=<runtime-name>
3. applyDeployment (SSA) — includes durable env vars when persistence is set
4. reconcileLeases — create/delete to match replica count (after Deployment, needs its UID)
5. applyService (SSA)
6. updateStatus — includes lease count and LeaseReady condition
```

Leases are reconciled after the Deployment so the Deployment's UID is available for the owner reference. Pods cannot become ready until leases exist (readiness gate), so this ordering is safe.

## Lease Specification

Each member lease matches what quarkus-flow's `LeaseService.createOrUpdateMemberLease` produces.

### Naming

```
flow-pool-member-{rt.Name}-{NN}
```

Where `NN` is zero-padded (e.g., `00`, `01`). Example for a Runtime `my-runtime` with 3 replicas:

- `flow-pool-member-my-runtime-00`
- `flow-pool-member-my-runtime-01`
- `flow-pool-member-my-runtime-02`

No leader lease is created. The operator replaces the leader's role entirely.

### Labels

| Label | Value |
|-------|-------|
| `app.kubernetes.io/managed-by` | `quarkus-flow` |
| `app.kubernetes.io/component` | `durable` |
| `io.quarkiverse.flow.durable.k8s/pool` | `{rt.Name}` |
| `io.quarkiverse.flow.durable.k8s/is-leader` | `false` |

### Spec

| Field | Value |
|-------|-------|
| `leaseDurationSeconds` | `30` |
| `holderIdentity` | not set (pods fill on acquisition) |
| `renewTime` | not set |
| `acquireTime` | not set |

### Owner Reference

Points to the Deployment (not the Runtime CR), matching quarkus-flow's `DeploymentPoolTopologyResolver` convention:

| Field | Value |
|-------|-------|
| `apiVersion` | `apps/v1` |
| `kind` | `Deployment` |
| `name` | `{rt.Name}` |
| `uid` | Deployment's UID |
| `controller` | `false` |

This ensures leases are garbage-collected when the Deployment is deleted.

### No SSA

Leases use standard `Create` and `Delete` — not Server-Side Apply. The operator creates leases but pods update them (setting `holderIdentity`, `renewTime`). SSA with ForceOwnership would conflict with the pods' updates. `AlreadyExists` errors on Create are ignored (idempotent).

## Replica Count Resolution

The effective replica count follows the existing `ApplicationSpec` precedence:

```go
func effectiveReplicas(app *logicv1.ApplicationSpec) int32 {
    if app.PodTemplate.Replicas != nil {
        return *app.PodTemplate.Replicas
    }
    if app.Replicas != nil {
        return *app.Replicas
    }
    return 1
}
```

HPA updates `spec.replicas` via the scale subresource, which triggers a reconcile. The controller reads the new value and adjusts lease count accordingly.

## Lease Reconciliation Logic

The `reconcileLeases` method:

1. Skip if `rt.Spec.Persistence == nil`
2. Compute `desired` = `effectiveReplicas(&rt.Spec.ApplicationSpec)`
3. Fetch the Deployment to get its UID for the owner reference
4. List existing leases with label `io.quarkiverse.flow.durable.k8s/pool={rt.Name}`
5. **Create** leases for indices `0..desired-1` that don't exist yet (ignore `AlreadyExists`)
6. **Delete** leases with indices `>= desired` (scale-down cleanup)

### Scale-down and workflow state

On scale-down, the operator deletes excess leases. Running or waiting workflows in the database associated with those leases are the user's responsibility to drain before scaling down. The LogicFlowService provides the traffic control lever for graceful drain.

## Container Env Vars

When persistence is configured, a `ContainerOption` sets these env vars on the container. All are immutable — user-provided duplicates are filtered out before the operator values are set.

### Operator-controlled

| Env Var | Value |
|---------|-------|
| `QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED` | `false` |
| `QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME` | `{rt.Name}` |

### Downward API

| Env Var | Source |
|---------|--------|
| `POD_NAME` | `fieldRef: metadata.name` |
| `POD_NAMESPACE` | `fieldRef: metadata.namespace` |

## Pod RBAC

Runtime pods need RBAC to acquire and renew leases. The operator manages a ServiceAccount and RoleBinding per Runtime, bound to a shared ClusterRole.

### ClusterRole: `logic-flow-runtime-durable`

Created by the operator on first reconcile of any Runtime with persistence, then reused. Each reconcile checks existence and creates if missing (idempotent). No owner reference — the ClusterRole is cluster-scoped and shared, not tied to any single Runtime's lifecycle. Permissions:

```yaml
rules:
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "update"]
```

No `create`, `delete`, or `patch` — the operator handles those. No `pods`, `deployments`, or `replicasets` access — the `DeploymentPoolTopologyResolver` is bypassed since the leader is disabled.

### Per-Runtime resources

| Resource | Name | Purpose |
|----------|------|---------|
| ServiceAccount | `{rt.Name}` | Identity for runtime pods |
| RoleBinding | `{rt.Name}-durable` | Binds ClusterRole to ServiceAccount in the Runtime's namespace |

The Deployment's pod spec gets `serviceAccountName` set to the ServiceAccount.

These resources are only created when `rt.Spec.Persistence != nil`. Owner references point to the LogicFlowRuntime CR so they are GC'd on Runtime deletion.

## Operator RBAC

New RBAC markers on the Runtime controller:

```go
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;delete
```

## Status Updates

### New condition: `LeaseReady`

| Status | Reason | When |
|--------|--------|------|
| `True` | `Ready` | All desired leases exist |
| `False` | `LeaseNotFound` | Some leases are missing |

Feeds into `DerivePhase` — Runtime won't reach `Ready` if leases are missing.

### New field: `LeaseReplicas`

```go
// LeaseReplicas is the number of durable pool leases.
// +optional
LeaseReplicas int32 `json:"leaseReplicas,omitempty"`
```

No lease refs list — the naming convention is deterministic, so individual lease names add no value. The count is enough.

## Files Modified

| File | Change |
|------|--------|
| `internal/controller/logicflowruntime_controller.go` | Add `reconcileLeases`, `reconcilePodRBAC`, update reconcile flow, new RBAC markers |
| `internal/controller/quarkus_config.go` | Add `WithDurableEnvVars` ContainerOption |
| `internal/controller/objects_common.go` | Add `effectiveReplicas`, lease builder helpers |
| `api/v1/logicflowruntime_types.go` | Add `LeaseReplicas` status field |
| `api/v1/status_types.go` | Add `ConditionLeaseReady`, `ReasonLeaseNotFound` |
| `internal/controller/logicflowruntime_controller_test.go` | New test contexts for leases, RBAC, durable env vars |
| `internal/controller/quarkus_config_test.go` | Unit tests for `WithDurableEnvVars` |
| `internal/controller/integration_test.go` | Integration test for durable + ConfigMap flow |

## Testing Strategy

### Unit tests

| Test | Assertion |
|------|-----------|
| `effectiveReplicas` | nil/explicit/podTemplate precedence, default=1 |
| `WithDurableEnvVars` | Sets all 4 env vars, filters user duplicates, immutability |
| `WithDurableEnvVars` nil persistence | Not called, no env vars |

### Envtest tests

| Test | Assertion |
|------|-----------|
| Lease creation with persistence | N leases exist with correct names, labels, spec, Deployment owner ref |
| No leases without persistence | Zero leases for minimal Runtime |
| Scale up (1→3) | 2 new leases created |
| Scale down (3→1) | Excess leases deleted |
| Idempotency | Repeated reconciliation produces same leases |
| LeaseReady condition | True when all leases exist |
| Durable env vars | All 4 env vars set when persistence configured |
| Leader disabled | `QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED=false` |
| Pod RBAC created | ServiceAccount, RoleBinding exist with persistence |
| Pod RBAC not created | No ServiceAccount/RoleBinding without persistence |
| ServiceAccountName on Deployment | Pod spec references created ServiceAccount |

### Integration test

Full flow: Runtime with persistence → leases created → Definition added → ConfigMap mounted + leases present → scale change → leases adjusted.
