# ClusterLogicPlatform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the namespaced `LogicPlatform` with a cluster-scoped singleton `ClusterLogicPlatform` that owns one Data Index, Gateway, and Vector per cluster, with three bring-your-own PostgreSQL datastores and runtimes that discover the platform cluster-wide.

**Architecture:** A new cluster-scoped CRD deploys components into an elected `targetNamespace`. Runtimes across the cluster associate via `platformRef` (auto-discovering the singleton when omitted) and inherit runtime/quartz persistence from the platform, or run ephemerally with the in-memory scheduler. A single per-platform DBMigrator Job (Flyway, DDL creds) migrates the configured datastores; operational pods use DML-only creds. The build reuses the existing component builders (`platform_dataindex.go`, `platform_gateway.go`, `platform_vector.go`) and the `quarkus_config.go` env-var pipeline.

**Tech Stack:** Go, controller-runtime, Kubebuilder, server-side-apply (`*ApplyConfiguration`), PostgreSQL/Flyway, Quarkus (quarkus-flow runtime image).

**Spec:** `adr/003-clusterlogicplatform-design.md`

## Global Constraints

- **No license headers on new files** — the repo adds them via an automated script (CLAUDE.md). Preserve existing Apache headers on files moved from the old implementation.
- **API group/version stays `logic.kubesmarts.org/v1`** (`PROJECT`, `api/v1`).
- **Concise API doc comments** — type-level 1–2 sentences; field-level one line; keep kubebuilder markers (CLAUDE.md).
- **Server-side apply**: new objects follow the existing `*ApplyConfiguration` builder pattern (`common_objects.go`, `platform_component.go`).
- **Persistence credentials are per-datastore**: each datastore carries a DDL `secretRef` (migration only) and a DML `secretRef` (operational).
- **Singleton**: at most one `ClusterLogicPlatform` may exist cluster-wide.
- **Migrator image** defaults to `quay.io/kubesmarts/logic-db-migrator:<release-tag>`, overridable via `spec.dbMigrator.image`.

## Review Focus

- **Second `ClusterLogicPlatform` created** → create webhook must reject it (Task 4), so a cluster can never present two platforms to Backstage.
- **Runtime with no `platformRef` when zero or two platforms exist** → zero = reject/requeue with clear status; two = impossible by Task 4, but the discovery path (Task 7) must still error rather than pick arbitrarily.
- **Two different runtimes sharing one quartz DB** → each must get a distinct, stable `SCHED_NAME`; identical replicas of one runtime must share it (Task 8).
- **Operational pod handed DDL credentials** → operational env vars must reference the DML `secretRef`, never the DDL one (Task 7, Task 9 split).
- **`targetNamespace` missing or deleted** → platform controller must surface a non-ready condition rather than silently no-op (Task 6).

---

## File Structure

- `api/v1/clusterlogicplatform_types.go` — new cluster-scoped CRD (created).
- `api/v1/clusterlogicplatform_webhook.go` — singleton validating webhook (created).
- `api/v1/persistence_types.go` — add DDL/DML split + per-store `DatastoreSpec` (modified).
- `api/v1/logicflowruntime_types.go` — add `platformRef`, replace raw persistence with mode (modified).
- `api/v1/logicflowruntime_webhook.go` — validate `platformRef`/mode (modified).
- `internal/controller/clusterlogicplatform_controller.go` — reworked from `logicplatform_controller.go` (created; old removed in Task 11).
- `internal/controller/platform_dbmigrator.go` — DBMigrator Job builder (created).
- `internal/controller/quarkus_config.go` — add quartz property env vars (modified).
- `internal/controller/logicflowruntime_controller.go` — platform resolution + inheritance (modified).
- Removal set (Task 11): `api/v1/logicplatform_types.go`, `api/v1/logicplatform_webhook.go`, `internal/controller/logicplatform_controller.go`, `internal/controller/logicplatform_objects.go`, `config/rbac/logicplatform_*`, `config/samples/*logicplatform*`.

---

## Task 1: `ClusterLogicPlatform` API types

**Files:**
- Create: `api/v1/clusterlogicplatform_types.go`
- Test: `api/v1/clusterlogicplatform_types_test.go`

**Interfaces:**
- Produces: `ClusterLogicPlatform`, `ClusterLogicPlatformSpec{ TargetNamespace string; DataIndex DataIndexSpec; Gateway ...; Vector ...; Runtime RuntimeDatastoreSpec; DBMigrator *DBMigratorSpec }`, `ClusterLogicPlatformStatus`, and `DBMigratorSpec{ Image string }`.
- Reuses `DataIndexSpec` and gateway/ingress specs from the current `LogicPlatform` (to be relocated, not duplicated).

- [ ] **Step 1:** Write failing test `TestClusterLogicPlatform_IsClusterScoped` asserting the generated CRD/type has `+kubebuilder:resource:scope=Cluster` behavior — e.g. a round-trip through the scheme and a marker check, plus `TestClusterLogicPlatformSpec_RequiresTargetNamespace` (empty `TargetNamespace` fails the validation helper).
- [ ] **Step 2:** Run tests; expect FAIL (types undefined).
- [ ] **Step 3:** Define the types in `api/v1/clusterlogicplatform_types.go` with markers `+kubebuilder:object:root=true`, `+kubebuilder:resource:scope=Cluster,shortName={clp}`, `+kubebuilder:subresource:status`. `TargetNamespace` is `+kubebuilder:validation:Required`. Add printcolumns for Data Index ready + target namespace. Keep doc comments concise (type-level example allowed for this top-level CRD).
- [ ] **Step 4:** Run `make generate manifests` (or project equivalent) and the tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(api): add ClusterLogicPlatform cluster-scoped CRD`.

## Task 2: Per-datastore persistence with DDL/DML split

**Files:**
- Modify: `api/v1/persistence_types.go`
- Test: `api/v1/persistence_types_test.go`

**Interfaces:**
- Produces: `DatastoreSpec{ PostgreSQL *PersistencePostgreSQL; MigrationSecretRef *PostgreSQLSecretOptions }` where `PersistencePostgreSQL.SecretRef` is the **DML** (operational) credential and `MigrationSecretRef` is the **DDL** credential; `RuntimeDatastoreSpec{ Runtime DatastoreSpec; Quartz DatastoreSpec }`.
- Consumes: existing `PersistencePostgreSQL`, `PostgreSQLSecretOptions`, `PostgreSQLServiceOptions`.

- [ ] **Step 1:** Write failing tests: `TestDatastoreSpec_OperationalSecretIsDML` (operational connection resolves `SecretRef`, not `MigrationSecretRef`) and `TestDatastoreSpec_MigrationSecretIsDDL` (migration path resolves `MigrationSecretRef`, falling back to `SecretRef` when unset so single-credential setups still work).
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Add `DatastoreSpec` and `RuntimeDatastoreSpec` and the resolver helpers `OperationalSecret(d DatastoreSpec) PostgreSQLSecretOptions` and `MigrationSecret(d DatastoreSpec) PostgreSQLSecretOptions`. One line of intent: migration falls back to operational secret only when `MigrationSecretRef` is nil.
- [ ] **Step 4:** Run tests + `make manifests`; expect PASS.
- [ ] **Step 5:** Commit: `feat(api): per-datastore DDL/DML credential split`.

## Task 3: `LogicFlowRuntime` platformRef + persistence mode

**Files:**
- Modify: `api/v1/logicflowruntime_types.go`
- Test: `api/v1/logicflowruntime_types_test.go`

**Interfaces:**
- Produces: `LogicFlowRuntimeSpec.PlatformRef *PlatformReference` where `PlatformReference{ Name string; Kind string }` (`Kind` `+kubebuilder:default=ClusterLogicPlatform`); `LogicFlowRuntimeSpec.PersistenceMode` enum `{PlatformDB, Ephemeral}` (`+kubebuilder:default=PlatformDB`). Remove `LogicFlowRuntimeSpec.Persistence` raw field.
- Consumes: nothing new.

- [ ] **Step 1:** Write failing tests: `TestLogicFlowRuntime_PlatformRefKindDefaultsToCLP`, `TestLogicFlowRuntime_EphemeralMeansNoPersistence` (mode=Ephemeral ⇒ `HasPersistence`-equivalent returns false), `TestLogicFlowRuntime_PlatformDBIsDefault`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Add the fields/enum and a helper `IsEphemeral(rt *LogicFlowRuntime) bool`. Remove the raw `Persistence` field. Keep field comments to one line.
- [ ] **Step 4:** Run tests + `make manifests`; expect PASS (compilation of controller will break — fixed in Task 7; keep this task's test green at the API layer).
- [ ] **Step 5:** Commit: `feat(api): LogicFlowRuntime platformRef and persistence mode`.

## Task 4: ClusterLogicPlatform singleton webhook

**Files:**
- Create: `api/v1/clusterlogicplatform_webhook.go`
- Test: `api/v1/clusterlogicplatform_webhook_test.go`

**Interfaces:**
- Consumes: a `client.Client` (list existing platforms).
- Produces: `ValidateCreate` rejecting a second platform; `ValidateCreate`/`ValidateUpdate` rejecting empty `TargetNamespace`.

- [ ] **Step 1:** Write failing tests: `TestValidateCreate_RejectsSecondPlatform` (one exists → error), `TestValidateCreate_AllowsFirstPlatform`, `TestValidate_RejectsEmptyTargetNamespace`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement the validating webhook following the pattern in the (to-be-removed) `logicplatform_webhook.go`; list `ClusterLogicPlatformList` and reject if any item other than the one under validation exists.
- [ ] **Step 4:** Run tests; expect PASS. Regenerate webhook manifests.
- [ ] **Step 5:** Commit: `feat(webhook): ClusterLogicPlatform singleton validation`.

## Task 5: LogicFlowRuntime platformRef/mode validation

**Files:**
- Modify: `api/v1/logicflowruntime_webhook.go`
- Test: `api/v1/logicflowruntime_webhook_test.go`

**Interfaces:**
- Produces: validation that `PersistenceMode=Ephemeral` forbids a `platformRef` that only makes sense for DB inheritance? No — `platformRef` is still used for Data Index association even when ephemeral. Validate instead: `Kind`, when set, equals `ClusterLogicPlatform`; `Name` non-empty when `platformRef` is present.

- [ ] **Step 1:** Write failing tests: `TestRuntime_RejectsUnknownPlatformKind`, `TestRuntime_AllowsOmittedPlatformRef` (auto-discovery path), `TestRuntime_RejectsPlatformRefWithEmptyName`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Add the checks to the existing runtime webhook.
- [ ] **Step 4:** Run tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(webhook): validate LogicFlowRuntime platformRef`.

## Task 6: ClusterLogicPlatform controller

**Files:**
- Create: `internal/controller/clusterlogicplatform_controller.go`
- Test: `internal/controller/clusterlogicplatform_controller_test.go`
- Reuse: `platform_dataindex.go`, `platform_gateway.go`, `platform_vector.go`, `platform_component.go`, `platform_objects`/`common_objects.go`.

**Interfaces:**
- Consumes: `ClusterLogicPlatformSpec` (Task 1), `OperationalSecret` (Task 2).
- Produces: component objects applied into `spec.targetNamespace`; status conditions `Ready`, `TargetNamespaceResolved`.

- [ ] **Step 1:** Write failing envtest `TestCLP_DeploysComponentsIntoTargetNamespace` (Data Index + Gateway Deployment + Vector DaemonSet land in `targetNamespace`) and `TestCLP_MissingTargetNamespace_SetsNotReady`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Port the reconcile loop from `logicplatform_controller.go`, changing: owner is cluster-scoped (ownerRef to cluster object on namespaced dependents is allowed for GC); all component namespaces come from `spec.targetNamespace`; Data Index persistence uses the operational (DML) secret.
- [ ] **Step 4:** Run tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(controller): ClusterLogicPlatform reconciler`.

## Task 7: Runtime platform resolution + persistence inheritance

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go`
- Test: `internal/controller/logicflowruntime_controller_test.go`

**Interfaces:**
- Consumes: `PlatformReference` (Task 3), `RuntimeDatastoreSpec` (Task 2), `IsEphemeral` (Task 3).
- Produces: `resolvePlatform(ctx, rt) (*ClusterLogicPlatform, error)` — uses `platformRef.Name` if set, else lists and returns the singleton, erroring if zero or (defensively) more than one. `effectiveRuntimePersistence(rt, platform) *DatastoreSpec` — nil when ephemeral.

- [ ] **Step 1:** Write failing tests: `TestResolvePlatform_UsesSingletonWhenRefOmitted`, `TestResolvePlatform_ErrorsWhenNoPlatform`, `TestRuntime_EphemeralUsesMinimalImageNoDBEnv`, `TestRuntime_PlatformDBInheritsRuntimeDatastore` (env vars built from the platform's `spec.runtime.db` operational secret).
- [ ] **Step 2:** Run; expect FAIL (and fix the compile break from Task 3 here).
- [ ] **Step 3:** Replace every `rt.Spec.Persistence` branch (image selection via `DefaultRunnerImage`, `WithQuarkusPersistenceEnvVars`, `reconcileLeases`, `reconcilePodRBAC`, `updateStatusLeases`) with the resolved platform datastore or the ephemeral path. Operational env vars use `OperationalSecret` (DML), never the migration secret.
- [ ] **Step 4:** Run tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(controller): runtime inherits persistence from platform`.

## Task 8: Quartz property injection

**Files:**
- Modify: `internal/controller/quarkus_config.go`
- Test: `internal/controller/quarkus_config_test.go`

**Interfaces:**
- Produces: `WithQuartzSchedulerEnvVars(rt *LogicFlowRuntime) ContainerOption` setting `QUARKUS_QUARTZ_INSTANCE_NAME`, `QUARKUS_QUARTZ_CLUSTERED`, `QUARKUS_QUARTZ_INSTANCE_ID`; and `schedName(rt) string` = `fmt.Sprintf("%s/%s", rt.Namespace, rt.Name)`.
- Consumes: nothing new; only applied when not ephemeral.

- [ ] **Step 1:** Write failing tests: `TestSchedName_IsNamespaceSlashName`, `TestQuartzEnv_ClusteredWhenReplicasGreaterThanOne`, `TestQuartzEnv_InstanceIdIsAuto`, `TestQuartzEnv_OmittedWhenEphemeral`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement the option and helper; wire it into the runtime deployment builder (Task 7's `applyDeployment`) only on the DB-backed path.
- [ ] **Step 4:** Run tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(controller): deterministic Quartz SCHED_NAME and clustering`.

## Task 9: DBMigrator Job

**Files:**
- Create: `internal/controller/platform_dbmigrator.go`
- Test: `internal/controller/platform_dbmigrator_test.go`

**Interfaces:**
- Consumes: `ClusterLogicPlatformSpec`, `MigrationSecret` (Task 2), `DBMigratorSpec.Image` (Task 1).
- Produces: `buildDBMigratorJob(platform *ClusterLogicPlatform) *batchv1ac.JobApplyConfiguration` — a Job in `targetNamespace` with one migration step per **distinct** configured datastore (Data Index, runtime, quartz), using each store's **DDL** secret; image defaults to `quay.io/kubesmarts/logic-db-migrator:<release-tag>` when `spec.dbMigrator.image` is empty.

- [ ] **Step 1:** Write failing tests: `TestMigratorJob_UsesDDLSecretPerStore`, `TestMigratorJob_DeduplicatesSharedInstance` (runtime+quartz on same instance/db ⇒ not double-migrated for identical targets), `TestMigratorJob_DefaultsImageToQuay`, `TestMigratorJob_HonorsImageOverride`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement the builder and call it from Task 6's reconcile (apply the Job, gate `Ready` on Job completion). One line of intent: dedupe by `(host,port,database,schema)`.
- [ ] **Step 4:** Run tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(controller): per-platform DBMigrator Job`.

## Task 10: Cross-namespace RBAC for runtime/Vector → Data Index

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go` (extend `reconcilePodRBAC`)
- Test: `internal/controller/logicflowruntime_controller_test.go`

**Interfaces:**
- Consumes: resolved platform `targetNamespace`, Data Index Secret name.
- Produces: a `Role` in `targetNamespace` granting read on the Data Index Secret and a `RoleBinding` for the runtime/Vector ServiceAccount; the operator creates/owns the ServiceAccount it binds.

- [ ] **Step 1:** Write failing tests: `TestRBAC_CreatesRoleBindingInTargetNamespace`, `TestRBAC_ServiceAccountCreatedBeforeBinding`.
- [ ] **Step 2:** Run; expect FAIL.
- [ ] **Step 3:** Implement the Role/RoleBinding apply in `targetNamespace`; ensure SA exists first.
- [ ] **Step 4:** Run tests; expect PASS.
- [ ] **Step 5:** Commit: `feat(controller): cross-namespace RBAC for Data Index access`.

## Task 11: Remove `LogicPlatform`

**Files:**
- Delete: `api/v1/logicplatform_types.go`, `api/v1/logicplatform_webhook.go`, `internal/controller/logicplatform_controller.go`, `internal/controller/logicplatform_objects.go`, `config/rbac/logicplatform_*.yaml`, `config/samples/*logicplatform*`.
- Modify: `config/crd/kustomization.yaml`, `config/rbac/kustomization.yaml`, `config/samples/kustomization.yaml`, `config/webhook/manifests.yaml`, `cmd`/manager setup wiring.

- [ ] **Step 1:** Grep for remaining `LogicPlatform` (type, not ClusterLogicPlatform) references; expect only deletions remain: `grep -rn "\bLogicPlatform\b" --include="*.go" api internal cmd`.
- [ ] **Step 2:** Remove the files and registrations; run `make generate manifests build`.
- [ ] **Step 3:** Run the full unit/envtest suite; expect PASS (no dangling references).
- [ ] **Step 4:** Commit: `refactor!: remove namespaced LogicPlatform in favor of ClusterLogicPlatform`.

## Task 12: Chart, samples, migration guide, demo Postgres

**Files:**
- Modify: `charts/logic-operator-stack/templates/` (replace `logic-platform.yaml` with a `ClusterLogicPlatform`), `dist/helm/crds.yaml`.
- Create: `config/samples/logic_v1_clusterlogicplatform.yaml`, a sample Postgres manifest + secret for `kind`/demo, `adr/migration/clusterlogicplatform-migration.md`.

- [ ] **Step 1:** Write/adjust a chart lint/test (or `helm template` smoke) asserting the rendered manifest is a `ClusterLogicPlatform` with `targetNamespace` set.
- [ ] **Step 2:** Run it; expect FAIL.
- [ ] **Step 3:** Update chart + samples; write the migration guide (delete old `LogicPlatform`, install `ClusterLogicPlatform` with `targetNamespace`=old namespace, re-point runtimes via `platformRef`); add the demo Postgres + secret (empty DBs, DDL + DML users) referenced by the sample.
- [ ] **Step 4:** Run `helm template`/lint and the demo path; expect PASS.
- [ ] **Step 5:** Commit: `docs,chart: ClusterLogicPlatform chart, samples, migration guide`.

---

## Self-Review Notes

- **Spec coverage:** CRD pivot (T1,4,6,11); three BYO datastores + DDL/DML (T2,9); runtime platformRef + mode (T3,5,7); quartz safety (T8); DBMigrator on Quay + override (T9); cross-ns RBAC (T10); migration/chart/demo (T12). Spin-off quarkus-flow quartz defaults are tracked as a **separate repo issue**, not in this plan.
- **Ordering:** API (T1–3) → webhooks (T4–5) → controllers (T6–10) → removal (T11) → delivery (T12). Task 3 intentionally breaks controller compilation; Task 7 restores it — execute in order.
- **Review Focus** inputs are each pinned to a task's tests (second platform→T4; discovery with 0/2 platforms→T7; shared quartz→T8; DDL-on-operational→T7/T9; missing targetNamespace→T6).
