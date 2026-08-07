# LogicFlowRuntime ConfigMap Integration Design

**Date:** 2026-08-07
**Status:** Accepted

## Context

The LogicFlowDefinition controller materializes workflow documents into labeled ConfigMaps (see `logicflowdefinition-controller.md`). The Runtime controller must discover these ConfigMaps, mount them as volumes on the Deployment, and reflect their state in status fields.

The Quarkus Flow runner loads workflows from `QUARKUS_FLOW_RUNNER_SOURCE_PATH` (default `/deployments/workflows`), recursively scanning for `.json`, `.yaml`, `.yml` files. Workflows are loaded once at startup -- ConfigMap changes require a pod restart (rolling update).

Key decisions:

- **ConfigMap watch:** The Runtime controller watches ConfigMaps filtered by the `logic.kubesmarts.org/runtime-ref` label. A mapper function reads the label value and enqueues the matching Runtime for reconciliation.
- **Volume-per-ConfigMap:** Each ConfigMap gets its own Volume + VolumeMount under a subdirectory of `/deployments/workflows/`. Subdirectory isolation prevents key collisions when two workflows share a file name.
- **No new conditions:** The existing `DeploymentAvailable` and `ServiceReady` conditions are sufficient. Volume mounting failures surface through Deployment conditions naturally.
- **Pod restart on change:** Adding or removing a ConfigMap changes the Deployment's volume list, triggering a rolling update. This is the desired behavior since Quarkus Flow loads workflows at startup only.

## Reconciliation Flow Changes

The existing flow is:

```
1. Fetch LogicFlowRuntime (IgnoreNotFound)
2. applyDeployment (SSA)
3. applyService (SSA)
4. updateStatus
```

The updated flow:

```
1. Fetch LogicFlowRuntime (IgnoreNotFound)
2. List ConfigMaps with label runtime-ref=<runtime-name>
3. applyDeployment (SSA) -- now includes volumes from step 2
4. applyService (SSA)
5. updateStatus -- now populates definitions[] and configMapRefs[]
```

The `applyConfigMap` stub (line 76-78) is removed. The Runtime does not create ConfigMaps -- it consumes them.

## ConfigMap Discovery

On each reconcile, the controller lists ConfigMaps in the Runtime's namespace with matching labels:

```go
var cmList corev1.ConfigMapList
err := r.List(ctx, &cmList,
    client.InNamespace(rt.Namespace),
    client.MatchingLabels{LabelRuntimeRef: rt.Name},
)
```

The returned list is passed to `applyDeployment` and `updateStatus`.

## Volume Mounting

For each discovered ConfigMap, the Deployment gets:

```yaml
volumes:
  - name: lfd-payment-processor-v1-0-0
    configMap:
      name: lfd-payment-processor-v1-0-0

containers:
  - name: logic-runner
    volumeMounts:
      - name: lfd-payment-processor-v1-0-0
        mountPath: /deployments/workflows/lfd-payment-processor-v1-0-0
        readOnly: true
```

This is implemented as a `ContainerOption` function `WithFlowVolumeMounts(configMaps []corev1.ConfigMap)` for the container-level volume mounts, and pod-level volumes are added to the `toPodSpecAC` output. Both integrate into the existing `ToDeploymentSpec` / `applyDeployment` flow.

### Source Path Env Var

The operator must set `QUARKUS_FLOW_RUNNER_SOURCE_PATH=/deployments/workflows` on the container to match the volume mount path. This env var is **not user-overridable** -- if a user sets it via the container spec, the operator's value takes precedence.

Implementation: a `ContainerOption` (`WithFlowSourcePath()`) that filters out any existing `QUARKUS_FLOW_RUNNER_SOURCE_PATH` entry from the env list before setting the operator-controlled value. This is necessary because Kubernetes uses the first occurrence of duplicate env var names.

### Volume Determinism

ConfigMaps are sorted by name before building volumes to ensure deterministic Deployment specs across reconcile loops. Without sorting, SSA would detect spurious changes and trigger unnecessary rolling updates.

### Mount Path Convention

```
/deployments/workflows/<configmap-name>/<data-key>
```

Example with two definitions targeting the same runtime:

```
/deployments/workflows/lfd-payment-processor-v1-0-0/payment-processor.json
/deployments/workflows/lfd-order-flow-v2-0-0/order-flow.json
```

The Quarkus runner scans recursively, so all workflows are discovered regardless of subdirectory depth.

## Status Update

Two status fields are populated from the discovered ConfigMaps:

### status.configMapRefs

```go
rt.Status.ConfigMapRefs = []corev1.LocalObjectReference{
    {Name: "lfd-payment-processor-v1-0-0"},
    {Name: "lfd-order-flow-v2-0-0"},
}
```

### status.definitions

Workflow metadata is extracted from ConfigMap labels (set by the Definition controller):

```go
rt.Status.Definitions = []RuntimeDefinitionStatus{
    {
        Name:    "payment-processor",  // from LabelWorkflowName
        Version: "1.0.0",             // from LabelWorkflowVersion
    },
}
```

The `Service` field is populated by the LogicFlowService controller, not by the Runtime controller.

## Controller Watch Changes

```go
func (r *LogicFlowRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&logicv1.LogicFlowRuntime{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.Service{}).
        Watches(&corev1.ConfigMap{},
            handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToRuntime),
            builder.WithPredicates(runtimeRefLabelPredicate()),
        ).
        Named("logicflowruntime").
        Complete(r)
}
```

### Mapper Function

```go
func (r *LogicFlowRuntimeReconciler) mapConfigMapToRuntime(ctx context.Context, obj client.Object) []reconcile.Request {
    rtName := obj.GetLabels()[LabelRuntimeRef]
    if rtName == "" {
        return nil
    }
    return []reconcile.Request{
        {NamespacedName: types.NamespacedName{Name: rtName, Namespace: obj.GetNamespace()}},
    }
}
```

### Label Predicate

A label predicate filters the ConfigMap informer to only cache ConfigMaps with the `runtime-ref` label, avoiding unnecessary memory and reconcile load from unrelated ConfigMaps.

## RBAC

The Runtime controller already has ConfigMap read access:

```go
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
```

No new RBAC markers needed.

## Files Modified

| File | Change |
|------|--------|
| `internal/controller/logicflowruntime_controller.go` | Add ConfigMap list, pass to applyDeployment, update status, add watch + mapper, remove applyConfigMap stub |
| `internal/controller/objects_common.go` | Add `WorkflowMountPath` constant, add volume/volumemount builder functions |
| `internal/controller/logicflowruntime_controller_test.go` | New test contexts for ConfigMap integration |

## Testing Strategy

Envtest-based tests following the existing pattern:

| Test Case | Assertion |
|-----------|-----------|
| No ConfigMaps exist | Deployment has no workflow volumes. status.definitions and configMapRefs are empty. |
| One ConfigMap with runtime-ref label | Deployment has one volume + volumeMount. status.definitions has one entry. status.configMapRefs has one entry. |
| Multiple ConfigMaps | Volumes are sorted by name. All definitions appear in status. |
| ConfigMap added after initial reconcile | Re-reconcile adds the volume and updates status. |
| ConfigMap removed | Re-reconcile removes the volume and updates status. |
| ConfigMap with wrong runtime-ref label | Not included in volumes or status. |
| Idempotency | Repeated reconciliation with same ConfigMaps produces identical Deployment spec. |
