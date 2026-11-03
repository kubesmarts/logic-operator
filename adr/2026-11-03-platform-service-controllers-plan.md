# Refactor LogicPlatform Reconciliation into Service Components — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the 1,374-line `logicplatform_controller.go` monolith into one self-contained file per platform component (Data Index, Vector, Workflow Gateway), behind a common `PlatformComponent` interface produced by a factory, with the controller acting as a thin orchestrator.

**Architecture:** One controller-runtime controller continues to watch `LogicPlatform`. Its `Reconcile` builds a slice of `PlatformComponent` implementations from a factory, runs each component's `Reconcile` **independently** (one failure does not stop the others), then runs each component's `UpdateStatus` (in-memory mutation of `plat.Status`), then writes `.status` **once**. Errors from all phases are aggregated with `errors.Join` so any failure still requeues. Each component owns its own build-vs-teardown decision from the spec; the factory always returns all components. New components (e.g. the upcoming DB migrator) are added by appending to the factory.

**Tech Stack:** Go 1.26, controller-runtime v0.22+ (native SSA via `client.Apply`), Ginkgo/Gomega + envtest, controller-runtime fake client (unit tests only).

**Spec:** GitHub issue #74 (`Refactor LogicPlatform reconciliation into service controllers`) + the brainstorm captured in this plan's Architecture and Design Decisions sections.

## Global Constraints

- Module path: `github.com/kubesmarts/logic-operator`
- No license/copyright headers on new files (an automated script adds them).
- **Commit policy (project rule):** Do NOT run `git commit` without the user's exact phrase `APPROVED: commit`. Each task's "Commit" step is staged and proposed, then blocks on that phrase.
- Use native SSA (`r.Apply` with apply configurations, `client.FieldOwner(FieldOwnerLogicOperator)`, `client.ForceOwnership`) for all child resources — never read-before-write. See `adr/implementation/controller-reconciliation-pattern.md`.
- Conditions carry `ObservedGeneration` and positive polarity; `.status` is observed, not constructed.
- **Behavior preservation:** this is a refactor. The existing Ginkgo suite (`logicplatform_controller_test.go`, `logicplatform_controller_workflow_gateway_test.go`) is the regression oracle and must stay green **unchanged** at every task boundary. Do not edit those test files except where a task explicitly says so.

## Design Decisions

Decisions already settled in brainstorming — implementers follow these, they are not open:

1. **Interface name is `PlatformComponent`**, not `Service` (avoids collision with `corev1.Service`, which these files use constantly).
2. **Two-phase contract:** `Reconcile` (drive resources) and `UpdateStatus` (mutate `plat.Status` in memory only — it must NOT call the API). The orchestrator performs the single `Status().Update`.
3. **Independent execution:** the orchestrator never short-circuits. Every component's `Reconcile` runs, then every component's `UpdateStatus` runs, regardless of earlier errors, so `.status` always reflects every component. Errors are collected and joined.
4. **Factory always returns all components.** Each component decides internally whether to build (enabled) or tear down + clear its status slice (disabled). No "only build the enabled ones" — that would split a component's logic across two places.
5. **`Finalizer() string` is on the interface** (returns `""` when the component needs none). Only Workflow Gateway returns a finalizer today. The gateway component keeps its existing finalizer + deletion-cleanup logic internally (behavior-preserving); the orchestrator additionally uses `Finalizer()` as a deletion-time backstop so the platform can never get stuck if a component forgets to remove its own finalizer.
6. **Components embed `*LogicPlatformReconciler`** so moved method bodies keep working (`c.Apply`, `c.Get`, `c.DatabaseConnector`, package-level helpers all resolve). Moving a method = move the func block + change receiver `(r *LogicPlatformReconciler)` → `(c *<component>)` + `r.` → `c.` in the body.
7. **Factory is an injectable seam** (`components` field on the reconciler, `nil` → default) so the orchestrator loop can be unit-tested with fake components and a fake client.

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/controller/platform_component.go` (new) | `PlatformComponent` interface; the `componentFactory` type + default factory; `(r *LogicPlatformReconciler) components()` accessor. |
| `internal/controller/platform_dataindex.go` (new) | `dataIndexComponent` + all Data Index reconcile, persistence, URL, and status methods. |
| `internal/controller/platform_vector.go` (new) | `vectorComponent` + all Vector reconcile (RBAC, ConfigMap, DaemonSet, Service, teardown) and status methods. |
| `internal/controller/platform_gateway.go` (new) | `workflowGatewayComponent` + all Workflow Gateway reconcile, teardown, finalizer, and status methods. |
| `internal/controller/logicplatform_controller.go` (modify) | Slim to: reconciler struct (+ `components` seam), RBAC markers, `Reconcile` orchestrator, `writeStatus`, finalizer backstop helper, cross-cutting status helpers (`platformComponentReadiness`, `updateLogicPlatformReadiness`, `removeLogicPlatformCondition`, `hasOwner`), `SetupWithManager`. |
| `internal/controller/platform_component_test.go` (new) | Unit test for the orchestrator loop (independent execution, single status write, error aggregation) using fake components + fake client. |

The `*_objects.go` / `*_builders.go` helper files (`logicplatform_objects.go`, `workflow_gateway_builders.go`, `common_objects.go`) are **not** moved — they already hold stateless builders split by concern and are out of scope for issue #74 (which targets the reconciliation flow). Leave them as-is.

## Review Focus

Inputs/behaviors the spec implies but the existing suite may not pin after the refactor — each gets a test in the owning task:

- **Independent execution:** a component whose `Reconcile` errors must not prevent later components' `Reconcile` or `UpdateStatus` from running, nor prevent the single status write. (Task 1 unit test.)
- **Error aggregation:** when ≥2 components fail, the returned error is non-nil and names each failing component (so requeue happens and logs are actionable). (Task 1 unit test.)
- **Single status write:** `Status().Update` is called exactly once per reconcile even with multiple components. (Task 1 unit test.)
- **Finalizer backstop on deletion:** when `plat.DeletionTimestamp` is set and teardown succeeds, no component finalizer remains on the object. (Covered by the existing gateway deletion test; Task 4 keeps it green.)
- **Disabled component clears its status slice:** a disabled component sets its `plat.Status.<X>` to nil and removes its conditions. (Covered by existing suite; Tasks 2–4 keep green.)

---

### Task 1: Interface, factory seam, and orchestrator

**Files:**
- Create: `internal/controller/platform_component.go`
- Create: `internal/controller/platform_component_test.go`
- Modify: `internal/controller/logicplatform_controller.go` (reconciler struct; rewrite `Reconcile`; extract `writeStatus`; extract Data Index status block into `updateDataIndexStatus`)

**Interfaces:**
- Produces:
  ```go
  type PlatformComponent interface {
      Name() string
      Finalizer() string // "" when the component needs no finalizer
      Reconcile(ctx context.Context, plat *logicv1.LogicPlatform) error
      UpdateStatus(ctx context.Context, plat *logicv1.LogicPlatform) error
  }

  // componentFactory builds the ordered set of components for one reconcile.
  type componentFactory func(r *LogicPlatformReconciler) []PlatformComponent

  func (r *LogicPlatformReconciler) components() []PlatformComponent // uses r.newComponents or defaultComponents
  ```
- The reconciler struct gains an injectable seam:
  ```go
  type LogicPlatformReconciler struct {
      client.Client
      Scheme            *runtime.Scheme
      DatabaseConnector DatabaseConnector
      newComponents     componentFactory // nil => defaultComponents
  }
  ```
- In Task 1, the three component types are **delegating wrappers** that embed `*LogicPlatformReconciler` and call the existing methods (`c.reconcileDataIndex`, `c.updateDataIndexStatus`, `c.reconcileVector`, `c.updateVectorStatus`, `c.reconcileWorkflowGateway`, `c.updateWorkflowGatewayStatus`). Tasks 2–4 relocate the bodies. `workflowGatewayComponent.Finalizer()` returns `FinalizerWorkflowGateway`; the others return `""`.

- [ ] **Step 1: Write the failing unit test** in `platform_component_test.go` (standard `testing`, no envtest). Use a `fakeComponent` implementing `PlatformComponent` with configurable `reconcileErr`/`statusErr` and call-recording. Build a `LogicPlatformReconciler` with `components` returning the fakes and a controller-runtime fake client holding a `LogicPlatform` with a status subresource.

  ```go
  func TestReconcile_RunsAllComponentsIndependently(t *testing.T) {
      // c1.Reconcile errors; c2 and c3 must still Reconcile and UpdateStatus.
      // Assert: every fake's Reconcile and UpdateStatus was called.
      // Assert: returned error is non-nil and contains c1.Name().
  }

  func TestReconcile_AggregatesErrorsFromBothPhases(t *testing.T) {
      // c1.Reconcile errors, c2.UpdateStatus errors.
      // Assert: returned error mentions both c1 and c2.
  }

  func TestReconcile_WritesStatusExactlyOnce(t *testing.T) {
      // Use a client wrapper counting Status().Update calls.
      // Assert: count == 1 even with 3 components.
  }
  ```

- [ ] **Step 2: Run tests to verify they fail**

  Run: `go test ./internal/controller/ -run 'TestReconcile_' -count=1`
  Expected: FAIL (compile error — `components`/interface not defined yet).

- [ ] **Step 3: Create `platform_component.go`** with the `PlatformComponent` interface, `componentFactory` type, `defaultComponents` (returns `&dataIndexComponent{r}`, `&vectorComponent{r}`, `&workflowGatewayComponent{r}`), the three delegating component structs (each `struct{ *LogicPlatformReconciler }` with `Name`/`Finalizer`/`Reconcile`/`UpdateStatus`), and `(r *LogicPlatformReconciler) components()`.

- [ ] **Step 4: Rewrite `Reconcile` and extract helpers in `logicplatform_controller.go`.**
  - Add the `components` field to the struct.
  - Extract the inline Data Index status block (current lines ~330–374 of `updateStatus`) into `func (r *LogicPlatformReconciler) updateDataIndexStatus(ctx, plat) error` (enabled/disabled branch, persistence + deployment + service sub-status, Data Index readiness rollup — in-memory only, no API write).
  - Extract the terminal write (current lines ~387–398) into `func (r *LogicPlatformReconciler) writeStatus(ctx, plat) error`: set `ObservedGeneration`, call `updateLogicPlatformReadiness(plat)`, then the `retry.RetryOnConflict` re-Get + `plat.Status = *saved` + `Status().Update`.
  - Replace the body of `Reconcile` with the orchestrator:
    ```go
    var plat logicv1.LogicPlatform
    if err := r.Get(ctx, req.NamespacedName, &plat); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    logicv1.ApplyLogicPlatformDefaults(&plat)

    comps := r.components()
    var errs []error
    for _, c := range comps {
        if err := c.Reconcile(ctx, &plat); err != nil {
            errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
        }
    }
    for _, c := range comps {
        if err := c.UpdateStatus(ctx, &plat); err != nil {
            errs = append(errs, fmt.Errorf("%s status: %w", c.Name(), err))
        }
    }
    if err := r.writeStatus(ctx, &plat); err != nil {
        errs = append(errs, err)
    }
    if plat.DeletionTimestamp != nil && len(errs) == 0 {
        if err := r.removeComponentFinalizers(ctx, &plat, comps); err != nil {
            errs = append(errs, err)
        }
    }
    return ctrl.Result{}, errors.Join(errs...)
    ```
  - Add `func (r *LogicPlatformReconciler) removeComponentFinalizers(ctx, plat, comps) error`: for each `c.Finalizer() != ""`, if present on `plat`, remove via merge patch (idempotent backstop).
  - Delete the now-unused monolithic `updateStatus` method (its Data Index block moved to `updateDataIndexStatus`; its Vector/Gateway calls become component `UpdateStatus`; its write became `writeStatus`). Keep `updateVectorStatus` and `updateWorkflowGatewayStatus` for now (Task 1 wrappers delegate to them).

- [ ] **Step 5: Run the new unit tests to verify they pass**

  Run: `go test ./internal/controller/ -run 'TestReconcile_' -count=1`
  Expected: PASS.

- [ ] **Step 6: Run the full controller suite to verify no regression**

  Run: `make test` (or `go test ./internal/controller/ -count=1` once envtest assets are set up)
  Expected: PASS, existing Ginkgo specs unchanged.

- [ ] **Step 7: Commit** (stage, propose message, wait for `APPROVED: commit`)

  ```bash
  git add internal/controller/platform_component.go internal/controller/platform_component_test.go internal/controller/logicplatform_controller.go
  # Proposed: "refactor(platform): introduce PlatformComponent interface and orchestrator (#74)"
  ```

---

### Task 2: Relocate Data Index into its own component file

**Files:**
- Create: `internal/controller/platform_dataindex.go`
- Modify: `internal/controller/logicplatform_controller.go` (remove relocated methods); `internal/controller/platform_component.go` (drop the delegating `dataIndexComponent`, it now lives in its file)

**Interfaces:**
- Consumes: `PlatformComponent` (Task 1), package-level helpers (`dataIndexName`, `objectKeyDataIndex`, `ChildLabelsInstance`, `SelectorLabels`, `OwnerRef`, `ToDeploymentSpec`, `ResolveIngressURL`, `BuildPostgresAddress`, `SetCondition`, `FindCondition`, `platformComponentReadiness`, `removeLogicPlatformCondition`).
- Produces: `type dataIndexComponent struct{ *LogicPlatformReconciler }` with `Name()="Data Index"`, `Finalizer()=""`, `Reconcile` = relocated `reconcileDataIndex`, `UpdateStatus` = relocated `updateDataIndexStatus`.

- [ ] **Step 1: Move the Data Index methods** from `logicplatform_controller.go` into `platform_dataindex.go`, changing each receiver `(r *LogicPlatformReconciler)` → `(c *dataIndexComponent)` and `r.` → `c.` in the bodies. Methods to move: `reconcileDataIndex`, `applyDataIndexDeployment`, `applyDataIndexService`, `applyDataIndexIngress`, `deleteDataIndexIngress`, `applyDataIndexKubernetesIngress`, `applyDataIndexRoute`, `updateDataIndexStatusDeployment`, `updateDeploymentStatusService`, `internalServiceURL`, `resolveExternalURL`, `updateStatus​Persistence`, `checkPersistenceConnectivity`, `checkPostgresDatabaseHealth`, and `updateDataIndexStatus` (from Task 1). Define `dataIndexComponent` + its `Name/Finalizer/Reconcile/UpdateStatus` here; remove the delegating version from `platform_component.go`.

- [ ] **Step 2: Build to verify it compiles**

  Run: `go build ./...`
  Expected: success, no "method redeclared" or "undefined" errors.

- [ ] **Step 3: Run the full suite to verify behavior unchanged**

  Run: `make test`
  Expected: PASS (same specs as Task 1).

- [ ] **Step 4: Commit** (stage, propose, wait for `APPROVED: commit`)

  ```bash
  git add internal/controller/platform_dataindex.go internal/controller/platform_component.go internal/controller/logicplatform_controller.go
  # Proposed: "refactor(platform): extract Data Index into its own component (#74)"
  ```

---

### Task 3: Relocate Vector into its own component file

**Files:**
- Create: `internal/controller/platform_vector.go`
- Modify: `internal/controller/logicplatform_controller.go`; `internal/controller/platform_component.go`

**Interfaces:**
- Produces: `type vectorComponent struct{ *LogicPlatformReconciler }` with `Name()="Vector"`, `Finalizer()=""`, `Reconcile` = relocated `reconcileVector`, `UpdateStatus` = relocated `updateVectorStatus`.

- [ ] **Step 1: Move the Vector methods** into `platform_vector.go` with receiver `(c *vectorComponent)`: `reconcileVector`, `applyVectorDaemonSet`, `applyVectorService`, `reconcileVectorRBAC`, `applyVectorConfigMap`, `deleteVectorResources`, `updateVectorStatus`. Define `vectorComponent` + its interface methods here; remove the delegating version from `platform_component.go`.

- [ ] **Step 2: Build**

  Run: `go build ./...`
  Expected: success.

- [ ] **Step 3: Run the full suite**

  Run: `make test`
  Expected: PASS.

- [ ] **Step 4: Commit** (stage, propose, wait for `APPROVED: commit`)

  ```bash
  git add internal/controller/platform_vector.go internal/controller/platform_component.go internal/controller/logicplatform_controller.go
  # Proposed: "refactor(platform): extract Vector into its own component (#74)"
  ```

---

### Task 4: Relocate Workflow Gateway into its own component file

**Files:**
- Create: `internal/controller/platform_gateway.go`
- Modify: `internal/controller/logicplatform_controller.go`; `internal/controller/platform_component.go`

**Interfaces:**
- Produces: `type workflowGatewayComponent struct{ *LogicPlatformReconciler }` with `Name()="Workflow Gateway"`, `Finalizer()=FinalizerWorkflowGateway`, `Reconcile` = relocated `reconcileWorkflowGateway`, `UpdateStatus` = relocated `updateWorkflowGatewayStatus`. The gateway keeps its internal `DeletionTimestamp`/finalizer handling verbatim; the orchestrator's `removeComponentFinalizers` (Task 1) is a backstop, not a replacement.

- [ ] **Step 1: Move the Gateway methods** into `platform_gateway.go` with receiver `(c *workflowGatewayComponent)`: `reconcileWorkflowGateway`, `applyGatewayServiceAccount`, `applyGatewayClusterRoleBinding`, `applyGatewayDeployment`, `applyGatewayService`, `applyGatewayIngress`, `deleteGatewayIngress`, `applyGatewayKubernetesIngress`, `applyGatewayRoute`, `deleteWorkflowGatewayResources`, `updateWorkflowGatewayStatus`. Define `workflowGatewayComponent` + its interface methods here; remove the delegating version from `platform_component.go`.

- [ ] **Step 2: Build**

  Run: `go build ./...`
  Expected: success.

- [ ] **Step 3: Run the full suite including the gateway deletion/finalizer specs**

  Run: `make test`
  Expected: PASS — in particular `logicplatform_controller_workflow_gateway_test.go` (finalizer added when enabled; resources cleaned and finalizer removed on delete).

- [ ] **Step 4: Commit** (stage, propose, wait for `APPROVED: commit`)

  ```bash
  git add internal/controller/platform_gateway.go internal/controller/platform_component.go internal/controller/logicplatform_controller.go
  # Proposed: "refactor(platform): extract Workflow Gateway into its own component (#74)"
  ```

---

### Task 5: Final cleanup and whole-branch verification

**Files:**
- Modify: `internal/controller/logicplatform_controller.go` (confirm it contains only orchestrator + cross-cutting helpers + setup)

**Interfaces:**
- Consumes: everything from Tasks 1–4. Produces nothing new.

- [ ] **Step 1: Confirm `logicplatform_controller.go` contents.** It should hold only: imports, `LogicPlatformReconciler` struct, RBAC `+kubebuilder` markers, `Reconcile`, `writeStatus`, `removeComponentFinalizers`, `platformComponentReadiness`, `updateLogicPlatformReadiness`, `removeLogicPlatformCondition`, `hasOwner`, and `SetupWithManager`. No component-specific apply/status methods remain. Remove any now-unused imports.

- [ ] **Step 2: Vet and lint**

  Run: `make fmt vet lint`
  Expected: clean.

- [ ] **Step 3: Full build + test**

  Run: `make build && make test`
  Expected: PASS.

- [ ] **Step 4: Commit** (stage, propose, wait for `APPROVED: commit`)

  ```bash
  git add internal/controller/logicplatform_controller.go
  # Proposed: "refactor(platform): slim orchestrator and tidy imports (#74)"
  ```

---

## Self-Review Notes

- **Spec coverage:** issue #74 asks for (a) focused per-service files — Tasks 2–4; (b) construction via a factory — Task 1 `componentFactory`/`defaultComponents`; (c) controller orchestrates and reports combined state — Task 1 orchestrator + `writeStatus`/`updateLogicPlatformReadiness`; (d) each service understandable independently — one file per component, no cross-calls. Covered.
- **Review Focus coverage:** independent execution, error aggregation, single status write → Task 1 unit tests; finalizer backstop + disabled-clears-status → existing Ginkgo specs kept green in Tasks 2–4.
- **Type consistency:** interface method set (`Name`/`Finalizer`/`Reconcile`/`UpdateStatus`) is identical across Tasks 1–4; `components()`/`componentFactory`/`components` names are used consistently.
- **Proportion:** the plan is shorter than the ~1,374-line file it reorganizes; code blocks are limited to the novel orchestrator algorithm, the interface, and the new unit tests. The bulk of the work is mechanical relocation guarded by the existing suite.
