# LogicFlowDefinition Controller Design

**Date:** 2026-08-07
**Status:** Accepted

## Context

The LogicFlowDefinition CRD holds an immutable Open Workflow Specification (OWS) 1.0.0 document and references the LogicFlowRuntime that executes it. The controller must materialize the flow document into a Kubernetes ConfigMap so the Runtime can discover and mount it.

Key decisions made during design:

- **ConfigMap ownership:** The Definition CR owns the ConfigMap (GC cascades on deletion).
- **Cross-controller coordination:** Label-based ConfigMap watch. The Runtime controller discovers ConfigMaps via a label selector, not by watching LogicFlowDefinition objects. Maximum decoupling.
- **RuntimeRef validation:** The controller validates that the referenced Runtime exists before creating the ConfigMap.
- **Flow parsing scope:** Parse the OWS document to extract metadata (name, version, namespace) for status fields. No deep structural validation (deferred to a future admission webhook).

## Reconciliation Flow

```
1. Fetch LogicFlowDefinition (IgnoreNotFound)
2. Validate runtimeRef -> GET LogicFlowRuntime
   - Missing? -> set condition RuntimeRefValid=False, skip ConfigMap, return
3. Parse flow document -> extract name, version, namespace
   - Parse error? -> set condition FlowParsed=False, skip ConfigMap, return
4. Apply ConfigMap (SSA) with:
   - Data key: <workflow-name>.json containing the raw flow JSON
   - Labels: runtime-ref, managed-by, workflow-name, workflow-version
   - OwnerReference -> LogicFlowDefinition (controller=true)
5. Update status:
   - ObservedGeneration, WorkflowName, WorkflowVersion, WorkflowNamespace
   - ConfigMapRef
   - Conditions: RuntimeRefValid=True, FlowParsed=True, ConfigMapReady=True
```

## ConfigMap Convention

Each LogicFlowDefinition produces one ConfigMap named `lfd-<definition-name>`.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: lfd-payment-processor-v1-0-0
  labels:
    logic.kubesmarts.org/runtime-ref: payments-runtime
    logic.kubesmarts.org/managed-by: logic-operator
    logic.kubesmarts.org/workflow-name: payment-processor
    logic.kubesmarts.org/workflow-version: "1.0.0"
  ownerReferences:
    - apiVersion: logic.kubesmarts.org/v1
      kind: LogicFlowDefinition
      name: payment-processor-v1-0-0
      controller: true
      blockOwnerDeletion: true
data:
  payment-processor.json: |
    { "document": { "dsl": "1.0.0", ... }, "do": [ ... ] }
```

### Label Schema

| Label | Value | Purpose |
|-------|-------|---------|
| `logic.kubesmarts.org/runtime-ref` | Runtime CR name | Runtime discovers ConfigMaps via this label |
| `logic.kubesmarts.org/managed-by` | `logic-operator` | Distinguishes operator-managed ConfigMaps |
| `logic.kubesmarts.org/workflow-name` | `flow.document.name` | Informational, useful for kubectl queries |
| `logic.kubesmarts.org/workflow-version` | `flow.document.version` | Informational |

## Conditions

| Condition | True | False |
|-----------|------|-------|
| `RuntimeRefValid` | Referenced LogicFlowRuntime exists | Runtime not found in namespace |
| `FlowParsed` | OWS document deserialized successfully | JSON parse error or missing document fields |
| `ConfigMapReady` | ConfigMap applied via SSA | SSA apply failed |

These conditions should be added as constants in `api/v1/status_types.go` alongside the existing Runtime conditions.

## Status Fields

```go
Status.ObservedGeneration  // last reconciled spec generation
Status.WorkflowName        // flow.document.name
Status.WorkflowVersion     // flow.document.version
Status.WorkflowNamespace   // flow.document.namespace (DSL namespace, not K8s)
Status.ConfigMapRef        // &LocalObjectReference{Name: "lfd-<definition-name>"}
Status.Conditions          // [RuntimeRefValid, FlowParsed, ConfigMapReady]
```

## Controller Watches

```go
func (r *LogicFlowDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&logicv1.LogicFlowDefinition{}).
        Owns(&corev1.ConfigMap{}).
        Named("logicflowdefinition").
        Complete(r)
}
```

- `For(LogicFlowDefinition)` -- primary watch.
- `Owns(ConfigMap)` -- re-reconcile if the owned ConfigMap is externally modified or deleted (self-healing).

No watch on LogicFlowRuntime. If a Runtime appears after a failed runtimeRef validation, the Definition won't automatically re-reconcile. Acceptable for v1: the user can edit the Definition or the next periodic resync handles it. A Runtime watch with a mapper can be added later if needed.

## RBAC

```go
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/finalizers,verbs=update
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
```

The Runtime RBAC marker adds read-only access to LogicFlowRuntimes (for runtimeRef validation).

## Impact on LogicFlowRuntime Controller

A follow-up change to the Runtime controller is needed to consume the ConfigMaps:

1. Add a `Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(mapConfigMapToRuntime))` that maps ConfigMaps with the `runtime-ref` label to Runtime reconcile requests.
2. On reconcile, list ConfigMaps with label `runtime-ref=<runtime-name>`.
3. Add volume and volumeMount entries to the Deployment for each discovered ConfigMap.
4. Update `status.definitions[]` and `status.configMapRefs[]`.

This is a separate task from the Definition controller implementation.

## Testing Strategy

Envtest-based tests following the LogicFlowRuntime pattern (direct `Reconcile()` calls):

| Test Case | Assertion |
|-----------|-----------|
| Valid flow + existing Runtime | ConfigMap created with correct labels, data, ownerRef. Status has workflowName, version, configMapRef. All conditions True. |
| RuntimeRef not found | No ConfigMap created. RuntimeRefValid=False. |
| Invalid flow JSON | No ConfigMap created. FlowParsed=False. |
| Spec update (flow changes) | ConfigMap data updated via SSA. Status reflects new metadata. |
| Idempotency | Repeated reconciliation produces identical state. |
| CR not found | Returns success, no ConfigMap created. |
| ConfigMap deleted externally | Owns() watch triggers re-reconcile, ConfigMap is recreated. |
