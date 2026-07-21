# Controller Reconciliation Pattern

**Status:** Design Document  
**Version:** v1.0  
**Last Updated:** 2026-07-21

## Overview

This document defines the controller reconciliation patterns for the Logic Operator. All controllers (LogicPlatform, LogicFlowRuntime, LogicFlowDefinition, LogicFlowService) follow these patterns for consistency, correctness, and maintainability.

The operator uses **Server-Side Apply (SSA)** for all child resource management, **OwnerReferences** for garbage collection, and **Conditions with ObservedGeneration** for status reporting.

## Table of Contents

1. [Server-Side Apply](#server-side-apply)
2. [Reconcile Loop Structure](#reconcile-loop-structure)
3. [Child Resource Management](#child-resource-management)
4. [Status Management](#status-management)
5. [Finalizer Pattern](#finalizer-pattern)
6. [Error Handling and Requeue](#error-handling-and-requeue)
7. [Controller Setup](#controller-setup)
8. [Testing Strategy](#testing-strategy)
9. [Reference Implementations](#reference-implementations)

---

## Server-Side Apply

### Why SSA Over CreateOrUpdate

The legacy `controllerutil.CreateOrUpdate` pattern (get-modify-create/update) has well-documented problems:

1. **Infinite reconcile loops** from defaulted fields (e.g., `ImagePullPolicy`, `TerminationGracePeriodSeconds`). Kubernetes defaults values on create, the next reconcile sees a diff, updates, Kubernetes re-defaults, ad infinitum.
2. **Optimistic concurrency conflicts** ("the object has been modified") when the cached object is stale, triggering exponential backoff.
3. **Complex merge logic** when multiple actors manage the same resource.

SSA eliminates all three: the API server tracks field ownership per manager, no-op writes don't bump `resourceVersion`, and defaulted fields are handled correctly because the server knows which fields you own.

cert-manager migrated fully to SSA by v1.21 after hitting these issues at scale. Helm 4 defaults to SSA. The Kubernetes project recommends SSA for all controllers.

### The SSA Pattern

```go
func (r *MyReconciler) applyDeployment(ctx context.Context, owner *v1.MyResource) error {
    deploy := &appsv1.Deployment{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "apps/v1",
            Kind:       "Deployment",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      owner.Name,
            Namespace: owner.Namespace,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: ptr.To(owner.Spec.Replicas),
            Selector: &metav1.LabelSelector{
                MatchLabels: map[string]string{"app": owner.Name},
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{"app": owner.Name},
                },
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "runtime",
                        Image: owner.Spec.Image,
                    }},
                },
            },
        },
    }

    if err := ctrl.SetControllerReference(owner, deploy, r.Scheme()); err != nil {
        return err
    }

    return r.Patch(ctx, deploy, client.Apply,
        client.FieldOwner(FieldOwnerLogicOperator),
        client.ForceOwnership,
    )
}
```

### SSA Rules

| Rule | Rationale |
|------|-----------|
| Always set `TypeMeta` (APIVersion + Kind) | SSA requires it; unlike normal CRUD, controller-runtime does not infer it |
| Always use `client.FieldOwner("logic-operator")` | Required for SSA; identifies which manager owns which fields |
| Always use `client.ForceOwnership` | The operator is authoritative over the fields it manages |
| Include ALL managed fields | Omitted fields are released/removed by SSA |
| Never read before write | Construct desired state from the CR spec, not from the live object |
| Set OwnerReference before applying | `ctrl.SetControllerReference` works with SSA |

### Field Owner Constant

All controllers use a single field owner to keep ownership consistent:

```go
const FieldOwnerLogicOperator = "logic-operator"
```

### SSA Gotchas

**Empty slices:** `subjects: []` on ClusterRoleBindings causes `resourceVersion` bumps on every apply (known Kubernetes issue). Omit empty slices instead of including them.

**Fake client:** SSA support in the fake client was buggy until controller-runtime v0.22.3. Use envtest (real API server) for integration tests, not the fake client.

**No-op writes are cheap:** When the desired state matches the live state, SSA does not write to etcd or broadcast to watchers. Applying every reconcile cycle is safe and correct.

---

## Reconcile Loop Structure

### Reconstructive Reconciler

Every reconcile cycle computes the full desired state from scratch. The reconciler never reacts to specific event types or diffs against the previous state.

```go
func (r *LogicFlowRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)

    // 1. Fetch the CR
    runtime := &v1.LogicFlowRuntime{}
    if err := r.Get(ctx, req.NamespacedName, runtime); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Handle deletion (finalizer, if needed)
    if !runtime.DeletionTimestamp.IsZero() {
        return r.reconcileDelete(ctx, runtime)
    }

    // 3. Reconcile child resources (SSA)
    if err := r.reconcileConfigMap(ctx, runtime); err != nil {
        return r.handleError(ctx, runtime, "ConfigMap", err)
    }
    if err := r.reconcileDeployment(ctx, runtime); err != nil {
        return r.handleError(ctx, runtime, "Deployment", err)
    }
    if err := r.reconcileService(ctx, runtime); err != nil {
        return r.handleError(ctx, runtime, "Service", err)
    }

    // 4. Observe child status and update conditions
    if err := r.updateStatus(ctx, runtime); err != nil {
        return ctrl.Result{}, err
    }

    return ctrl.Result{}, nil
}
```

### Principles

1. **Level-triggered, not edge-triggered.** Always ask "is the world in the desired state?" and drive toward it. Never react to specific events.
2. **Idempotent.** Running the same reconcile twice with no spec changes produces no side effects.
3. **Single responsibility per reconcile method.** Each child resource gets its own `reconcileX` method that constructs and applies the desired state.
4. **Status is observed, not constructed.** After applying child resources, read their current state to compute conditions.

---

## Child Resource Management

### OwnerReferences and Garbage Collection

Use `ctrl.SetControllerReference` for all in-namespace child resources. Kubernetes GC automatically deletes children when the parent CR is deleted.

```go
if err := ctrl.SetControllerReference(owner, child, r.Scheme()); err != nil {
    return err
}
```

- Only one controller reference per object (enforced by `SetControllerReference`).
- Cross-namespace ownership is not supported by Kubernetes. Use finalizers for cross-namespace or external cleanup.

### Deleting Stale Children

SSA does not delete child resources that should no longer exist. When the CR spec changes such that a child is no longer needed, delete it explicitly:

```go
func (r *Reconciler) deleteStaleResources(
    ctx context.Context,
    owner *v1.MyResource,
    desired sets.Set[string],
) error {
    existing := &corev1.ConfigMapList{}
    if err := r.List(ctx, existing,
        client.InNamespace(owner.Namespace),
        client.MatchingLabels{labelOwner: owner.Name},
    ); err != nil {
        return err
    }
    for i := range existing.Items {
        if !desired.Has(existing.Items[i].Name) {
            if err := r.Delete(ctx, &existing.Items[i]); err != nil && !apierrors.IsNotFound(err) {
                return err
            }
        }
    }
    return nil
}
```

### Standard Labels

All child resources carry consistent labels for querying and identification:

```go
func childLabels(owner metav1.Object) map[string]string {
    return map[string]string{
        "app.kubernetes.io/name":       owner.GetName(),
        "app.kubernetes.io/managed-by": "logic-operator",
        "app.kubernetes.io/part-of":    "logic-platform",
    }
}
```

---

## Status Management

### Conditions as Primary Mechanism

Conditions are the single source of truth for resource state. Phase, if used, is derived from conditions.

```go
func (r *Reconciler) setCondition(obj *v1.MyResource, condType string, status metav1.ConditionStatus, reason, message string) {
    meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
        Type:               condType,
        Status:             status,
        ObservedGeneration: obj.Generation,
        LastTransitionTime: metav1.Now(),
        Reason:             reason,
        Message:            message,
    })
}
```

### Condition Rules

| Rule | Detail |
|------|--------|
| Always set `ObservedGeneration` | GitOps tools (Argo CD, Flux) check `observedGeneration == metadata.generation` before considering a rollout complete |
| Positive polarity | `Status: True` = normal/healthy |
| PascalCase Reasons | `AllReplicasReady`, `DeploymentNotFound` (machine-readable) |
| Human-readable Messages | Descriptions for `kubectl describe` output |
| Conditions are additive | Never remove a condition type once added; set it to `True`/`False`/`Unknown` |

### Standard Condition Types

Each CRD defines its own condition types. Common ones:

| Condition | Meaning |
|-----------|---------|
| `Ready` | Overall resource readiness (the rollup condition) |
| `DeploymentReady` | Managed Deployment has available replicas |
| `ServiceReady` | Managed Service exists and is configured |
| `ConfigReady` | Configuration (ConfigMaps/Secrets) is valid and applied |

### Updating Status

Use the status subresource. Prefer `Patch` over `Update` to reduce conflict probability:

```go
func (r *Reconciler) updateStatus(ctx context.Context, obj *v1.MyResource) error {
    obj.Status.ObservedGeneration = obj.Generation
    return r.Status().Update(ctx, obj)
}
```

### Deriving Phase from Conditions

If a Phase field is useful for user ergonomics, derive it from conditions:

```go
func derivePhase(conditions []metav1.Condition) Phase {
    if meta.IsStatusConditionTrue(conditions, "Ready") {
        return PhaseRunning
    }
    if meta.IsStatusConditionPresentAndEqual(conditions, "Ready", metav1.ConditionFalse) {
        return PhaseFailed
    }
    return PhaseProvisioning
}
```

Phase is never maintained as independent state. It is always a function of conditions.

---

## Finalizer Pattern

### When to Use

- External resources (cloud infrastructure, DNS records, databases)
- Cross-namespace resources that OwnerReferences cannot handle
- Cleanup that must happen before the CR is removed from etcd

### When NOT to Use

- In-namespace child resources (Deployments, Services, ConfigMaps) — use OwnerReferences instead

### Implementation

```go
const finalizerName = "logic.kubesmarts.org/cleanup"

func (r *Reconciler) reconcileDelete(ctx context.Context, obj *v1.MyResource) (ctrl.Result, error) {
    if !controllerutil.ContainsFinalizer(obj, finalizerName) {
        return ctrl.Result{}, nil
    }

    if err := r.cleanupExternalResources(ctx, obj); err != nil {
        // Retry cleanup — use RequeueAfter, not error, to avoid exponential backoff
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
    }

    controllerutil.RemoveFinalizer(obj, finalizerName)
    if err := r.Update(ctx, obj); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}

func (r *Reconciler) ensureFinalizer(ctx context.Context, obj *v1.MyResource) error {
    if controllerutil.ContainsFinalizer(obj, finalizerName) {
        return nil
    }
    controllerutil.AddFinalizer(obj, finalizerName)
    return r.Update(ctx, obj)
}
```

### Rules

- Each controller manages its own finalizer and ignores others.
- On cleanup failure, return `RequeueAfter` (not an error) to avoid exponential backoff on repeated cleanup retries.
- Document an operational escape hatch for stuck finalizers (`kubectl edit` to remove manually).

---

## Error Handling and Requeue

### Return Patterns

| Return | Behavior | When to use |
|--------|----------|-------------|
| `ctrl.Result{}, nil` | Done, no requeue | Reconciliation succeeded |
| `ctrl.Result{RequeueAfter: d}, nil` | Requeue after duration, bypasses rate limiter | Waiting for external resource, polling interval |
| `ctrl.Result{}, err` | Log error, requeue with exponential backoff (5ms base, ~1000s cap) | Transient failures (API errors, network) |
| `ctrl.Result{Requeue: true}, nil` | Immediate requeue through rate limiter | Rare — prefer `RequeueAfter` with explicit duration |

Never return both `Requeue: true` and an error. The error already implies requeue with backoff.

### Error Classification

```go
func (r *Reconciler) handleError(
    ctx context.Context,
    obj *v1.MyResource,
    component string,
    err error,
) (ctrl.Result, error) {
    if isPermanentError(err) {
        // Invalid configuration — don't retry, set condition
        r.setCondition(obj, "Ready", metav1.ConditionFalse,
            "InvalidConfiguration", err.Error())
        _ = r.Status().Update(ctx, obj)
        return ctrl.Result{}, nil
    }

    if apierrors.IsConflict(err) || apierrors.IsServerTimeout(err) {
        // Transient — let backoff handle it
        return ctrl.Result{}, err
    }

    // Default: transient
    return ctrl.Result{}, err
}
```

### Error Categories

| Category | Action | Example |
|----------|--------|---------|
| **Permanent** | Set condition, don't retry | Invalid image reference, missing required field |
| **Transient** | Return error (automatic backoff) | API server timeout, network blip, conflict |
| **Rate-limited** | `RequeueAfter` with explicit delay | Cloud API throttling |
| **Not found (parent)** | `client.IgnoreNotFound(err)` | CR deleted between event and reconcile |

---

## Controller Setup

### Watching Resources

```go
func (r *LogicFlowRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&v1.LogicFlowRuntime{}).
        Owns(&appsv1.Deployment{}).
        Owns(&corev1.Service{}).
        Owns(&corev1.ConfigMap{}).
        WithEventFilter(predicate.GenerationChangedPredicate{}).
        Complete(r)
}
```

- `For()` — primary resource (the CR).
- `Owns()` — child resources. Automatically sets up `EnqueueRequestForOwner`, so changes to children trigger reconciliation of the parent.
- `Watches()` with `handler.EnqueueRequestsFromMapFunc` — non-owned resources (e.g., a shared Secret referenced by multiple CRs).
- `WithEventFilter(predicate.GenerationChangedPredicate{})` — skip reconciles triggered by status-only updates. Only fires when `.metadata.generation` changes (spec changes).

### Non-Owned Resource Watches

When a controller needs to react to resources it doesn't own (e.g., LogicFlowService watching LogicFlowDefinitions):

```go
ctrl.NewControllerManagedBy(mgr).
    For(&v1.LogicFlowService{}).
    Watches(
        &v1.LogicFlowDefinition{},
        handler.EnqueueRequestsFromMapFunc(r.findServicesForDefinition),
    ).
    Complete(r)
```

### Rate Limiter Customization

The default backoff is 5ms base, ~1000s cap. Adjust if needed:

```go
ctrl.NewControllerManagedBy(mgr).
    WithOptions(controller.Options{
        RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(
            200*time.Millisecond, // base delay
            5*time.Minute,        // max delay
        ),
    }).
    Complete(r)
```

---

## Testing Strategy

### Layered Approach

| Layer | Tool | What it tests |
|-------|------|---------------|
| **Unit** | Table-driven Go tests | Pure business logic: label computation, condition derivation, path building |
| **Integration** | envtest (real API server + etcd, no kubelet) | Reconciliation loops, CRD validation, owner references, status updates |
| **E2E** | KinD cluster | Full stack: built-in controllers, webhooks, real workloads |

### envtest Integration Tests

envtest is the recommended approach. The controller-runtime team explicitly discourages the fake client for integration testing.

```go
var testEnv *envtest.Environment
var k8sClient client.Client
var ctx context.Context
var cancel context.CancelFunc

var _ = BeforeSuite(func() {
    ctx, cancel = context.WithCancel(context.TODO())

    testEnv = &envtest.Environment{
        CRDDirectoryPaths: []string{
            filepath.Join("..", "..", "config", "crd", "bases"),
        },
    }
    cfg, err := testEnv.Start()
    Expect(err).NotTo(HaveOccurred())

    err = v1.AddToScheme(scheme.Scheme)
    Expect(err).NotTo(HaveOccurred())

    k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
    Expect(err).NotTo(HaveOccurred())

    mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme.Scheme})
    Expect(err).NotTo(HaveOccurred())

    err = (&LogicFlowRuntimeReconciler{
        Client: mgr.GetClient(),
        Scheme: mgr.GetScheme(),
    }).SetupWithManager(mgr)
    Expect(err).NotTo(HaveOccurred())

    go func() { mgr.Start(ctx) }()
})
```

### Async Assertions

Reconciliation is asynchronous. Always use `Eventually` / `Consistently`:

```go
It("should create a Deployment", func() {
    Eventually(func(g Gomega) {
        deploy := &appsv1.Deployment{}
        g.Expect(k8sClient.Get(ctx, types.NamespacedName{
            Name:      "my-runtime",
            Namespace: "default",
        }, deploy)).To(Succeed())
        g.Expect(deploy.Spec.Replicas).To(Equal(ptr.To(int32(3))))
    }).Should(Succeed())
})
```

### envtest Limitations

- Does NOT run kubelet or built-in controllers (Deployment controller, ReplicaSet controller).
- Cannot assert that Pods are created from Deployments.
- Assert only on resources your controller directly creates.
- Does NOT run webhooks by default (must be configured explicitly with `WebhookInstallOptions`).

---

## Reference Implementations

### Best-in-Class Operators to Study

| Operator | Key Patterns | Link |
|----------|-------------|------|
| **cert-manager** | SSA migration, multi-controller coordination, challenge retry patterns | [github.com/cert-manager/cert-manager](https://github.com/cert-manager/cert-manager) |
| **CloudNativePG** | Custom Pod Controller (bypasses StatefulSet), direct PVC management, instance manager | [github.com/cloudnative-pg/cloudnative-pg](https://github.com/cloudnative-pg/cloudnative-pg) |
| **Crossplane** | Composition engine, provider/managed resource pattern, multi-tenant | [github.com/crossplane/crossplane](https://github.com/crossplane/crossplane) |
| **Cluster API** | Conditions with ObservedGeneration, contract-based design, multi-provider | [github.com/kubernetes-sigs/cluster-api](https://github.com/kubernetes-sigs/cluster-api) |
| **External Secrets** | Clean SSA usage, provider abstraction | [github.com/external-secrets/external-secrets](https://github.com/external-secrets/external-secrets) |

### Libraries

| Library | Purpose | Notes |
|---------|---------|-------|
| **reconciler.io/runtime** | Sub-reconcilers + table-driven testing on top of controller-runtime | VMware origin (projectriff). Useful for breaking complex reconciliation into composable pieces. |
| **kro.run** | Declarative resource composition without writing controllers | For simple "glue" operators only. Not suitable for operators with complex business logic. |

### Key References

- [Kubernetes Blog: Server-Side Apply Is Great And You Should Be Using It](https://kubernetes.io/blog/2022/10/20/advanced-server-side-apply/)
- [Kubebuilder Good Practices](https://book.kubebuilder.io/reference/good-practices.html)
- [controller-runtime CreateOrUpdate ergonomics issue #2733](https://github.com/kubernetes-sigs/controller-runtime/issues/2733)
