# DBMigrationStrategy: Job-Based Schema Migration Design

**Date:** 2026-08-25
**Status:** Proposed

## Context

`PersistenceOptionsSpec.DBMigrationStrategy` already exists in the API (`api/v1/persistence_types.go`) with three intended values — `service`, `job`, `none` — but nothing implements it. Today, schema management is entirely the Quarkus Flow runner's baked-in `hibernate.database.generation=update`, regardless of what `DBMigrationStrategy` is set to. `persistenceEnvVars()` (`internal/controller/quarkus_config.go`) only builds JDBC connectivity env vars; it never reads `DBMigrationStrategy`.

With v2 running 3+ replicas against shared PostgreSQL, relying on `update` in production is fragile: multiple pods can race to alter schema on startup, there's no rollback, no migration versioning or audit trail, and `update` only ever adds columns — it never removes or modifies constraints.

This design implements the `job` strategy across three schema-bearing components that a `LogicPlatform` deployment can involve: the Quarkus Flow runner (workflow execution state), Data Index (workflow query/observability store), and Quartz (scheduler state, when enabled). Migration is driven entirely from `LogicPlatform` — never from individual `LogicFlowRuntime` resources directly (see Migration Ownership below). Not every deployment uses all three components; the design is delivered as three additive steps matching common combinations, so each is independently shippable and the operator never has to support a component that doesn't exist yet.

### Upstream dependency

This design assumes the capability proposed in the companion ADR in the `quarkus-flow` repository (`adr/2026-08-25-db-migration-extension-design.md`): a `quarkus-flow-db-migration` extension shipping versioned Flyway scripts for the JPA runtime schema and, separately, the Quartz schema — each invoked in "migrate-only" mode (apply migrations, exit 0/non-zero, don't start the app). The operator does not own or vendor any SQL — it only knows how to run a Job against the appropriate image and wait for the container to exit. If that upstream property/mechanism ends up named or shaped differently, only the Job's container command/env in this design needs to change; the reconciliation, status, and RBAC design is independent of the exact invocation contract.

Data Index already has its own migration module today — `logic-apps/data-index/data-index-storage/data-index-storage-migrations`, a real Flyway module with `V1__initial_schema.sql` — so the SQL itself is not a gap the way it is for quarkus-flow's JPA runtime schema. The module is a plain Flyway-core library today (no Quarkus extension, no Dockerfile of its own), pulled into `data-index-service-postgresql` as an ordinary Maven dependency for dev/test only — production Flyway is explicitly disabled there (`%prod.quarkus.flyway.migrate-at-start=false`), with a comment stating production schema is left to unspecified "external migration tools."

The fix mirrors what the companion quarkus-flow ADR does for the runtime/Quartz schemas: `logic-apps` packages `data-index-storage-migrations` as its own Quarkus extension, the same shape as `quarkus-flow-db-migration-runtime`/`-quartz`, rather than exposing the raw `.sql` files as a pullable artifact for an init container to mount. `db-migrator` takes a compile-time dependency on that extension the same way it does on quarkus-flow's — see Migrator Application, "Two ways to supply migration scripts." Step 2 therefore carries the same upstream-capability gap as Step 1 and Step 3: the SQL scripts exist, but the Quarkus-extension packaging that lets `db-migrator` bundle them at build time still needs to be built in `logic-apps`.

## Problem

What this actually costs the person deploying `LogicPlatform`/`LogicFlowRuntime` CRs today:

1. **The field lies.** `dbMigrationStrategy` is present in the CRD schema right now. Setting it to `job` or `none` is accepted by the API server and silently does nothing — schema management stays `update` regardless. This is worse than the field not existing: a user reads the field's doc comment, sets `job` expecting managed migrations, and gets none, with no error or warning anywhere.
2. **Production schema changes are a race today.** At 3+ replicas against shared PostgreSQL, every pod attempts `update` independently on startup. There's no operator-level coordination — a rolling restart or a scale-up event can mean several pods altering schema concurrently.
3. **No pre-flight signal.** If a schema change is going to fail (bad migration, insufficient DB privileges, connectivity issue), the user currently finds out via pods crash-looping with a buried Hibernate/JDBC stack trace in `kubectl logs`, not from a status condition they can check before rollout.
4. **No audit trail.** There's no record of what schema state a given deployment expects or when it last changed — `kubectl describe logicflowruntime` today says nothing about schema at all.
5. **No safe upgrade path when a schema needs to shrink.** `update` only adds; a column rename or constraint tightening requires manual DBA intervention outside the operator entirely, with no supported way to hand that work to a Job the operator manages.

## Migration Ownership: `LogicPlatform` Only

`DBMigrationStrategy` is honored **only** at `LogicPlatform` — never at an individual `LogicFlowRuntime` directly. `LogicPlatformReconciler` is the only reconciler that ever creates a `LogicDbMigration`: one for the runtime/Quartz schema stream, sourced from `LogicPlatformSpec.RuntimeDefaults.Persistence`, and one for the Data Index schema stream, sourced from `LogicPlatformSpec.DataIndex.Persistence`. Every `LogicFlowRuntime` in the namespace shares the same runtime-schema migration rather than each triggering its own.

**Why.** Most deployments have one `LogicPlatform` governing many `LogicFlowRuntime`s in a namespace. Letting each Runtime independently trigger, poll, and report its own migration Job multiplies moving parts — reconcile paths, RBAC, CRs — for something that's naturally a single Platform-wide concern: the runtime schema is one schema, shared by every Runtime that doesn't explicitly point elsewhere.

**Mechanism.** There is no formal binding between a `LogicFlowRuntime` and a specific `LogicPlatform` — no `PlatformRef` field exists. `LogicPlatformSpec.RuntimeDefaults` already assumes the convention this design relies on: one `LogicPlatform` supplies defaults "for all `LogicFlowRuntime` deployments in this namespace." `LogicFlowRuntimeReconciler` formalizes that convention as a namespace-scoped `List` for `LogicPlatform` (`client.InNamespace(rt.Namespace)`), expecting 0 or 1 result:
- **0 Platforms:** nothing to gate on — the Runtime deploys immediately, equivalent to `service`/`none`.
- **1 Platform:** the expected case — mirror its runtime-migration condition into the Runtime's own status.
- **>1 Platforms:** ambiguous. A validating webhook rejecting a second `LogicPlatform` per namespace removes the ambiguity structurally (see Open Questions).

**Field semantics.** `LogicFlowRuntimeSpec` embeds `RuntimeSpec`, and `LogicPlatformSpec.RuntimeDefaults` is typed as that same `RuntimeSpec` — so `Persistence.DBMigrationStrategy` is technically still settable directly on a `LogicFlowRuntime`, but the operator never reads it for migration purposes there; only `LogicPlatform.Spec.RuntimeDefaults.Persistence.DBMigrationStrategy` (runtime/Quartz) and `LogicPlatform.Spec.DataIndex.Persistence.DBMigrationStrategy` (Data Index) are honored. Two changes prevent this from silently recreating Problem #1 one level down: `PersistenceOptionsSpec`'s doc comment is updated to state that `DBMigrationStrategy` specifically does not cascade to the flow-specific level (only `PostgreSQL` connection fields do), and `LogicFlowRuntime`'s admission webhook (`api/v1/logicflowruntime_webhook.go`) emits a non-blocking Warning when `Persistence.DBMigrationStrategy` is set to a non-default value directly on a Runtime, pointing at `LogicPlatform.Spec.RuntimeDefaults` instead.

**Accepted limitation.** A `LogicFlowRuntime` that overrides `Persistence.PostgreSQL` away from what `RuntimeDefaults` resolves to — pointing at a different Postgres instance or schema — is not covered by `job`-strategy migration at all: the Platform only ever migrates the JDBC target its own `RuntimeDefaults.Persistence` resolves to.

## User Experience

**Before:** A user sets `dbMigrationStrategy: job` somewhere in `LogicPlatform`/`LogicFlowRuntime`, expecting the operator to manage schema migrations. Nothing happens — the field is ignored. The Deployment rolls out immediately, running `hibernate.database.generation=update` exactly as if `dbMigrationStrategy` had never been set. If a schema problem exists, the user's first signal is pods crash-looping after rollout, and diagnosing it means reading Hibernate stack traces out of pod logs.

**After:**
- Setting `dbMigrationStrategy: job` on `LogicPlatform.Spec.RuntimeDefaults.Persistence` actually does something: on the next reconcile, `LogicPlatformReconciler` creates a `LogicDbMigration`, which in turn creates a Job. Every `LogicFlowRuntime` in that namespace picks this up — `kubectl get logicflowruntime` / `kubectl describe` shows a `MigrationComplete` condition, `False`/`MigrationJobRunning` while it's in flight, `True` once schema is confirmed up to date. No Deployment (and therefore no pods) exist until that condition is `True` — a failed migration is visible *before* any pod starts, as `MigrationComplete=False`/`MigrationJobFailed`. Setting `dbMigrationStrategy` directly on an individual `LogicFlowRuntime` has no effect — only the Platform-level field does anything (see Migration Ownership).
- `kubectl get logicdbmigrations` / `kubectl describe logicdbmigration <platform-name>-runtime-migration` gives the actual report — which schema version was applied, how many migrations ran — not just pass/fail, closing the "no audit trail" gap (Problem #4, see Migration Report). This survives independently of the Job's own `ttlSecondsAfterFinished` GC window.
- First-time rollout of a `job`-strategy Platform takes somewhat longer (time for the Job to run) — the trade-off is explicit and documented, not a surprise. Subsequent reconciles where nothing changed re-run against an already-migrated schema and complete quickly (Flyway is idempotent — "0 pending migrations" is a fast, successful Job).
- Users who set `dbMigrationStrategy: none` get an explicit, visible statement of intent ("schema is externally managed") instead of silently getting `update` regardless of what they asked for — the runtime is configured with `hibernate.database.generation=none`, so an unexpected schema drift from Hibernate auto-DDL can no longer happen by accident.
- Users who don't touch the field at all (`service`, the default) see zero behavior change — today's `update` semantics continue exactly as before. Nobody is forced to adopt Job-based migration to keep working.
- Once Steps 2 and 3 land, the same experience — a status condition to check, a `LogicDbMigration` report to inspect on failure, no silent no-op — extends to Data Index and to Quartz-enabled runtimes, rather than being a one-off special case for the flow schema alone.

## Delivery Roadmap

| Step | Adds | Hard prerequisite | Prerequisite lives in | Independently shippable? |
|---|---|---|---|---|
| **1. Runtime schema migration** | Job-based migration for `LogicPlatform.Spec.RuntimeDefaults.Persistence` (JPA workflow schema), gating every `LogicFlowRuntime` in the namespace | quarkus-flow `quarkus-flow-db-migration-runtime` artifact, the unified `db-migrator` app (companion ADR; see Migrator Application), and a migration-reconciliation path in `LogicPlatformReconciler` (today a complete no-op stub) | Script artifact: existing `quarkus-flow` repo, new Maven module alongside the runner. Migrator app: new `db-migrator` module in the `logic-apps` repo (see Migrator Application). `LogicPlatformReconciler` scaffolding: this repo (`logic-operator`) | Partially — needs the migration piece of `LogicPlatformReconciler` implemented, but not the full Data Index Deployment/Service reconciliation Step 2 needs |
| **2. + Data Index migration** | Job-based migration for `LogicPlatform.Spec.DataIndex.Persistence` | Two: **(a)** `LogicPlatformReconciler` must reconcile Data Index at all — gating a Deployment that the operator doesn't create yet is meaningless. **(b)** `logic-apps` needs to package `data-index-storage-migrations` as a Quarkus extension `db-migrator` can depend on at build time (see Migrator Application's "bundled" mode). | (a) This repo (`logic-operator`). (b) Both the extension and the `db-migrator` module live in `logic-apps` — a same-repo dependency, no cross-repo publish needed for this piece (unlike the `quarkus-flow` dependency Step 1 needs) | No — blocked on both Data Index reconciliation landing (ADR 001 EPIC 6) and `logic-apps` publishing a consumable migration extension, neither of which this ADR designs |
| **3. + Quartz migration** | Extends Step 1's Job to also apply the Quartz schema stream when Quartz scheduling is enabled | **The operator's API has no Quartz concept at all today** (zero references anywhere in the Go code). A Platform-level signal for "runtimes governed by this Platform use Quartz" needs to exist before the migration side has anything to key off of. | This repo (`logic-operator`) for the API field only — the unified migrator app already bundles Quartz scripts once the companion ADR ships them, so no new image work is needed in `db-migrator` for this step | No — blocked on a scheduler-configuration API that doesn't exist and isn't designed here |

Each step below is written so it stands alone: what it adds, what must already be true, what changes, and what "done" looks like. Steps 2 and 3 both depend on the `LogicDbMigration` CRD and its shared controller introduced in Step 1 — designed once, reused twice — rather than being separate mechanisms (see `LogicDbMigration` Controller).

**One new module, `db-migrator`, is needed for this roadmap — living inside the existing `logic-apps` repo, not a new standalone repository — and only one new application, not one per schema.** Every migration Job in every step uses the same image, `db-migrator` (see Migrator Application below), built once from that module. All three steps use it in its "bundled" mode — `-runtime`/`-quartz` dependencies published from `quarkus-flow`, `-data-index` wired in-repo from `logic-apps`'s own Data Index extension — so `logic-apps` is responsible for building a real Quarkus extension for Data Index, not just publishing `data-index-storage-migrations`'s SQL files as a pullable artifact. No repo besides the existing `logic-apps` and `quarkus-flow` is touched; `logic-apps` gains a new module and a new publishable artifact, `quarkus-flow` only gains the latter.

## Strategy Semantics

Applies identically to every schema stream (runtime, Data Index, Quartz) — the strategy is a property of `PersistenceOptionsSpec`, and the operator only reads it from `LogicPlatform` (`RuntimeDefaults.Persistence` for runtime/Quartz, `DataIndex.Persistence` for Data Index), per Migration Ownership above.

| Strategy | Operator action | Component env |
|---|---|---|
| `service` (default) | No-op — today's behavior | `hibernate.database.generation=update` (current default, unchanged) |
| `job` | Create migration Job before the component's Deployment; block the Deployment until it succeeds | `hibernate.database.generation=none` — Flyway already established the schema; Hibernate must not also attempt DDL |
| `none` | No-op | `hibernate.database.generation=none` — schema is externally managed (DBA/Terraform/CI) |

`job` and `none` result in the *same* runtime env var — the difference is entirely whether the operator ran a migration Job first. `DBMigrationStrategy` therefore needs to be read in two places: once in the Job-creation path (`job` only, by `LogicPlatformReconciler`), once when building each component's env vars (`job` and `none` both force `database.generation=none`; `service` leaves today's default alone).

## Migrator Application: `db-migrator`

One application, not one per schema. Every migration Job in every step of this design — Step 1's runtime schema, Step 2's Data Index schema, Step 3's Quartz schema — runs the same container image, `db-migrator`, built and published from a new module inside the `logic-apps` repo (`logic-apps/db-migrator`). It's a small, generic, schema-agnostic Flyway runner: connect to a database, run `migrate()` against whichever scripts it's told about, exit with the result.

### Why inside `logic-apps`, not a new repo

`logic-apps` already owns Data Index's migration scripts (`data-index-storage-migrations`) and already depends on published `quarkus-flow` artifacts elsewhere — that's the standing dependency direction (`quarkus-flow` is the generic, Quarkiverse-published framework; `logic-apps` is a consumer, never the reverse). A `db-migrator` module living inside `logic-apps` fits both facts directly: its dependency on Data Index's own extension is in-repo module wiring (no publish/pull step at all), and its dependency on `quarkus-flow-db-migration-runtime`/`-quartz` is the same kind of cross-repo dependency `logic-apps` already takes on `quarkus-flow` elsewhere — nothing new to justify. The trade-off is that `db-migrator`'s build/release rides on `logic-apps`'s CI, presumably following whatever per-module image build pattern `logic-apps` already uses for `data-index-service-postgresql`/`data-index-ingestion-kafka-service` rather than a fully independent pipeline — exact versioning story is Not Designed Here (see Open Questions).

### Why one app instead of one per schema

Building and maintaining one shared migrator is simpler than one per schema: one image for the operator to know about and wait on across every step, one Flyway version to patch, one base image to keep current, one CVE surface — not two or three. The one thing this shared app can't do is take a compile-time dependency on `quarkus-flow`'s extensions from *inside* `quarkus-flow` itself — `quarkus-flow` is a generic framework `logic-apps` depends on, never the reverse, so an app living inside `quarkus-flow` could never also depend on a `logic-apps`-owned artifact like the Data Index extension without inverting that relationship. Living inside `logic-apps` sidesteps this entirely: `logic-apps` is already the side of that relationship allowed to depend on `quarkus-flow`, so `db-migrator` inherits that permission for free instead of needing a third, dependency-neutral repo to sit outside both.

### Two ways to supply migration scripts

**Bundled (published dependency)** — the default, and the mechanism for every schema this design covers, not only the ones `quarkus-flow` owns. `db-migrator` depends on `quarkus-flow-db-migration-runtime` and `quarkus-flow-db-migration-quartz` (the companion ADR's extension modules) as published Maven artifacts pulled cross-repo from `quarkus-flow`, and on `logic-apps`'s own Data Index migration extension as an ordinary in-repo module dependency — no publish/pull step needed for that one, since both live in `logic-apps`. All three always ship inside the one image, landing on the classpath at their isolated locations (`db/migration/flow-runtime`, `db/migration/flow-quartz`, `db/migration/data-index`). Which of them Flyway actually applies against a given database in a given invocation is controlled by an env var, not by what's on the classpath — everything bundled is always present in the image; the invocation just picks the subset to run. Picking up a new `quarkus-flow` script means bumping `db-migrator`'s dependency version and cutting a new image release, coupling `quarkus-flow`'s release cadence to the migrator's for schema changes; a new Data Index script just needs the in-repo dependency bumped as part of `logic-apps`'s own build (see Open Questions).

**Mounted (runtime-supplied)** — kept as a fallback for any future schema owner that can't or hasn't yet published a proper extension; not used by any of Steps 1–3 today. Flyway supports filesystem-based migration locations alongside classpath ones, so the mechanism (an extra `locations` entry pointed at a mounted directory, populated by a Kubernetes init container the operator adds to the Job) stays available in `LogicDbMigrationSpec.ScriptSource` without a live consumer, rather than being designed away entirely — cheap to keep, and one less thing to redesign if a future component can't get a build-time dependency published in time.

### Container contract

| Input | Source | Applies to |
|---|---|---|
| JDBC URL, credentials, TLS | Same env vars `persistenceEnvVars()` already produces | All steps |
| Which bundled schema(s) to apply | `LOGIC_DB_MIGRATOR_INCLUDE` (name TBD) — `runtime`, `quartz`, `data-index`, or a comma-combination (`runtime,quartz` for Step 3) | All steps |
| Externally-supplied scripts (Mounted mode) | A volume mounted at a well-known path (e.g. `/migrations`), populated by an init container; the migrator adds it as an extra Flyway `locations` entry when present. No current consumer — see Two ways to supply migration scripts | None today |
| Exit code | `0` — success, including "nothing to migrate"; non-zero — any Flyway failure | All steps |
| Migration report | Structured JSON written to the container's termination message (`/dev/termination-log`) on both success and failure — see Migration Report below | All steps |

### Migration Report

Exit code alone answers "did it work," not "what's the schema's current state." The migrator writes a small JSON summary to its termination message before exiting; `LogicDbMigrationReconciler` reads it off the finished Pod (`pod.status.containerStatuses[].state.terminated.message`) in the same reconcile that observes the Job's terminal state, and copies it into `LogicDbMigrationStatus.Streams`.

```json
{
  "streams": [
    { "name": "runtime", "appliedVersion": "5", "appliedCount": 2, "pendingBefore": 2 }
  ]
}
```

One entry per stream applied in that invocation — two entries once Step 3's combined `INCLUDE=runtime,quartz` Job lands, so a failure can say *which* stream failed instead of just "the Job failed" (see Step 3). On failure the same shape applies with whatever streams completed before the error, plus an `error` field naming the stream and underlying Flyway failure.

This deliberately reuses a plain Kubernetes mechanism (termination message) rather than giving the migrator its own Kubernetes client/RBAC to write a ConfigMap or similar — consistent with `db-migrator` being "a small, generic, schema-agnostic Flyway runner" with no Kubernetes awareness of its own. The container needs `terminationMessagePolicy: File` (the default) for this to work; `buildMigrationJob` sets it explicitly rather than relying on the cluster default. Exact JSON field names/versioning are Not Designed Here, same footing as the env var names above (see Open Questions).

### Job shape per step

| Step | Containers | Script source |
|---|---|---|
| 1 (runtime) | One: `db-migrator`, `INCLUDE=runtime` | Bundled |
| 2 (Data Index) | One: `db-migrator`, `INCLUDE=data-index` — same single-container shape as Step 1, no init container | Bundled |
| 3 (+ Quartz) | Same single container as Step 1, `INCLUDE=runtime,quartz` — no image or Job-shape change from Step 1, just a different env value | Bundled |

Since all three bundled streams always ship in the one image, no step needs a new image variant or new resolution logic beyond what's already there — only the `INCLUDE` value differs per Job, and Step 3's `runtime,quartz` combination only applies once the Quartz-configuration API this design depends on (see Step 3 below) exists to tell the operator to set it. Every step's Job has exactly one container.

### Naming and versioning

The migrator's own version moves independently of the runner's and of `quarkus-flow`'s — it only needs to change when a bundled schema stream changes, the migrator's own logic changes, or its Flyway dependency is bumped. Most runner and `quarkus-flow` releases won't touch it at all. Whether `db-migrator`, as a module inside `logic-apps`, gets its own independent version/tag scheme (the way `data-index-service-postgresql` and `data-index-ingestion-kafka-service` presumably already do as separate images from the same repo) or ends up piggybacked on a broader `logic-apps` release is a `logic-apps`-side decision this ADR doesn't make (see Open Questions) — the operator only needs to know its image reference, not its versioning policy.

### Not designed here

The migrator's Maven module layout, Java implementation, and exact env var names live entirely in the new `db-migrator` module in `logic-apps` — this ADR specifies the contract the operator needs (image, env vars in, exit code out, an optional mounted volume for externally-owned schemas), not the implementation. `db-migrator` itself is not created as part of this change — creating the module and its initial scaffolding within `logic-apps` is a prerequisite this ADR assumes but doesn't perform. The companion quarkus-flow ADR needs a follow-up update to reflect this direction — including that the app lives in `logic-apps`'s `db-migrator` module rather than inside `quarkus-flow`.

## API Changes

`DBMigrationStrategy` (`api/v1/persistence_types.go:91`) is currently typed as plain `string`, even though the three-value enum `DBMigrationStrategyType` already exists two lines above it (unused). Fix, required before any step:

```go
// DB Migration approach for the target application.
// job: Operator runs a Kubernetes Job to migrate the schema before starting the runtime.
// service: The runtime manages its own schema (Hibernate database.generation=update).
// none: Schema is externally managed; the operator does nothing.
// NOTE: only honored via LogicPlatform.Spec.RuntimeDefaults.Persistence and
// LogicPlatform.Spec.DataIndex.Persistence — ignored when set directly on a
// LogicFlowRuntime (see Migration Ownership).
// +optional
// +kubebuilder:default:=service
// +kubebuilder:validation:Enum=service;job;none
DBMigrationStrategy DBMigrationStrategyType `json:"dbMigrationStrategy,omitempty"`
```

This changes the generated CRD's field type (the wire format — a string — doesn't change), so `make manifests` needs to regenerate `config/crd/bases/*.yaml`.

`PersistenceOptionsSpec`'s own type-level doc comment currently describes a 3-level cascading-override model for the whole type (platform-global → runtime-defaults → flow-specific). It needs a follow-up wording fix stating that `DBMigrationStrategy` specifically does not cascade to the flow-specific level — only the `PostgreSQL` connection fields do.

### New field: `MigrationImage` (proposed)

`PersistenceOptionsSpec` also needs an optional override for the migrator image. Since every component uses the same `db-migrator` image by default (see Migrator Application), this is a single well-known default, not a per-component resolution — the field exists only so an advanced user can point at a custom or forked migrator image if they need to:

```go
// MigrationImage overrides the container image used for schema migration Jobs
// when DBMigrationStrategy is job. If unset, defaults to the operator's
// well-known db-migrator image (see Migrator Application) —
// the same image is used for every component (runner, Data Index, Quartz).
// +optional
MigrationImage string `json:"migrationImage,omitempty"`
```

This is a new field this design introduces — distinct from the `DBMigrationStrategy` fix above, which corrects a field that already exists.

### New webhook behavior: `LogicFlowRuntime` Persistence.DBMigrationStrategy warning

`api/v1/logicflowruntime_webhook.go` gains a non-blocking admission Warning when a `LogicFlowRuntime` sets `Persistence.DBMigrationStrategy` to a non-default value directly — pointing the user at `LogicPlatform.Spec.RuntimeDefaults.Persistence.DBMigrationStrategy` instead, since the field is never read for migration purposes at the Runtime level (see Migration Ownership).

## `LogicDbMigration` CRD

A status-bearing child CRD, one per schema-bearing stream that has `DBMigrationStrategy: job` — `<platform-name>-runtime-migration` (Step 1, later also carrying Step 3's Quartz stream) and `<platform-name>-data-index-migration` (Step 2). Both are owned by the same `LogicPlatform` (see Migration Ownership). It exists so "what's the current migration state of this schema" is a directly queryable, structured object instead of text buried in the owner's own condition `Message` — closing the audit-trail gap noted in Problem #4 for real, not just with a pass/fail bit.

**Operator-owned only.** `spec` is written exclusively by `LogicPlatformReconciler` (see `LogicDbMigration` Controller below) — users don't author or edit it directly, only read it (`kubectl get logicdbmigrations`). No validating/defaulting webhook is needed for the same reason: the only writer is the operator itself.

**Ownership chain:** `LogicPlatform` → `LogicDbMigration` (owner ref, `controller=true`) → `batchv1.Job` (owner ref points at the `LogicDbMigration`, not at the Platform directly) → `Pod`. Deleting the parent Platform cascades through the migration CR to the Job. `LogicFlowRuntime` is never an owner or a creator of any `LogicDbMigration` — it only reads the Platform's condition.

```go
const LogicDbMigrationKind = "LogicDbMigration"

// LogicDbMigrationSpec is the desired schema migration for one stream owned by
// a LogicPlatform. Written only by LogicPlatformReconciler — never intended
// for direct user authorship.
type LogicDbMigrationSpec struct {
    // Component identifies which schema this migration targets: runtime, data-index.
    // +required
    Component string `json:"component"`

    // Image is the db-migrator image to run. Defaults to the operator's
    // well-known image unless Persistence.MigrationImage overrides it.
    // +required
    Image string `json:"image"`

    // Include lists bundled schema streams to apply (e.g. ["runtime"], ["data-index"], ["runtime","quartz"]).
    // Mutually exclusive with ScriptSource.
    // +optional
    Include []string `json:"include,omitempty"`

    // ScriptSource configures the init container that supplies externally-owned
    // migration scripts for a component with no published extension yet.
    // Unused by Steps 1-3 today (see Two ways to supply migration scripts).
    // Mutually exclusive with Include.
    // +optional
    ScriptSource *MountedScriptSource `json:"scriptSource,omitempty"`

    // ConnectionSecretRef / JDBC target fields — the same values persistenceEnvVars()
    // already resolves from the owning Platform's PersistenceOptionsSpec.
}

// MigrationPhase summarizes a LogicDbMigration's current state.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type MigrationPhase string

const (
    MigrationPhasePending   MigrationPhase = "Pending"
    MigrationPhaseRunning   MigrationPhase = "Running"
    MigrationPhaseSucceeded MigrationPhase = "Succeeded"
    MigrationPhaseFailed    MigrationPhase = "Failed"
)

type LogicDbMigrationStatus struct {
    // Phase summarizes the migration Job's current state.
    // +optional
    Phase MigrationPhase `json:"phase,omitempty"`

    // JobRef names the batchv1.Job currently backing this migration (see Naming).
    // +optional
    JobRef string `json:"jobRef,omitempty"`

    // Streams reports the outcome per schema stream, parsed from the migrator
    // container's termination message (see Migration Report). One entry for
    // single-stream Jobs (Steps 1/2 alone), two once Step 3's combined
    // runtime+quartz Job lands.
    // +optional
    Streams []MigrationStreamStatus `json:"streams,omitempty"`

    // Conditions follows the standard metav1.Condition pattern; MigrationComplete
    // mirrors into the owner's own status (see Step 1/2 Status sections).
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// MigrationStreamStatus is one schema stream's result within a migration Job.
type MigrationStreamStatus struct {
    Name           string       `json:"name"` // "runtime", "quartz", "data-index"
    AppliedVersion string       `json:"appliedVersion,omitempty"`
    AppliedCount   int32        `json:"appliedCount,omitempty"`
    LastAppliedAt  *metav1.Time `json:"lastAppliedAt,omitempty"`
}
```

Field list above is illustrative, adapted directly from the `migrationJobInputs` struct this supersedes (see `LogicDbMigration` Controller) — exact JSON tags/types are not the point of contention here and can be refined during implementation.

## `LogicDbMigration` Controller

Rather than have `LogicPlatformReconciler` create and poll `batchv1.Job`s directly for each stream, it delegates to a new, dedicated `LogicDbMigrationReconciler` — the only place that builds Jobs, polls them, and harvests their report. This is the key design choice that makes Steps 2/3 additive instead of parallel reimplementations, and it's what makes the migration report a first-class, queryable object instead of a string wedged into someone else's condition. `LogicFlowRuntimeReconciler` never calls into this path at all — it only reads the Platform's mirrored condition (see Migration Ownership).

### Execution model

Every migration Job is a single Kubernetes `batchv1.Job` running one Pod to completion (`restartPolicy: Never`) — not a long-running service. What runs inside it — the `db-migrator` image, its bundled vs. mounted script modes, its env var and exit-code contract — is specified in full in Migrator Application above; this section covers the operator-side mechanics of building, polling, and reporting on that Job, which are identical regardless of which step is invoking them.

`internal/controller/logicdbmigration_controller.go` (new file) owns the reconcile loop for `LogicDbMigration` objects:

```
1. Fetch LogicDbMigration
2. buildMigrationJob(spec) / migrationJobName(spec) — same builder logic as before, now internal to this controller
3. Found, Succeeded: harvest report (read the Job's Pod, parse its termination message — see Migration Report)
   → Status.Streams, Phase=Succeeded, MigrationComplete=True
4. Found, Failed: harvest whatever partial report exists
   → Phase=Failed, MigrationComplete=False/MigrationJobFailed
5. Found, running: Phase=Running, requeue short backoff (e.g. 5s)
6. Not found: create the Job, Phase=Pending→Running
```

Harvesting happens in the same reconcile that first observes the Job's terminal state — well before `ttlSecondsAfterFinished` (see Container Spec below) has any chance to garbage-collect the Job/Pod, so there's no race between "the report exists" and "the Job it came from is still readable."

`internal/controller/migrationjob_objects.go` (new file) keeps the builder shape, now called only by `LogicDbMigrationReconciler`:

```go
type migrationJobInputs struct {
    Owner        *logicv1.LogicDbMigration  // owner ref target for the Job
    Namespace    string
    Image        string
    Include      []string                   // e.g. ["runtime"], ["data-index"], ["runtime","quartz"]
    ExtraEnv     []*corev1ac.EnvVarApplyConfiguration
    ScriptSource *MountedScriptSource        // non-nil only for a component with no published extension yet;
                                              // unset by Steps 1-3 today (see Two ways to supply migration scripts)
}

// MountedScriptSource configures the init container that populates externally-owned
// migration scripts for a component whose schema isn't bundled into the migrator image.
// Kept as a fallback mechanism; no current caller among Steps 1-3.
type MountedScriptSource struct {
    InitImage string // the image publishing the .sql files
    MountPath string // shared mount point, e.g. /migrations
}

func buildMigrationJob(in migrationJobInputs) *batchv1.Job
func migrationJobName(in migrationJobInputs) string // deterministic hash-based name, see below
```

`buildMigrationJob` adds an init container and an `emptyDir` volume (mounted into both the init and main containers) only when `ScriptSource != nil` — none of Steps 1–3 set it today; all three run a single container in Bundled mode.

`LogicPlatformReconciler` builds and upserts one `LogicDbMigrationSpec` per stream it's responsible for (`runtime` in Step 1, `data-index` in Step 2) from its own CR, then reads back each `LogicDbMigration.Status.Phase`/`Conditions` to decide whether to proceed with the corresponding Deployment — it never touches a `batchv1.Job` itself. Step 3 doesn't add a new builder or a new `LogicDbMigration` — it only changes what `Include` the Step 1 stream passes (`["runtime","quartz"]` instead of `["runtime"]`) on the *same* migration object.

### Naming

Kubernetes Jobs are immutable — `spec.template` can't be updated on an existing Job. If the JDBC target, schema, or image changes, a *new* Job is needed. The `LogicDbMigration` object's own name stays fixed per stream (`<platform-name>-runtime-migration`, `<platform-name>-data-index-migration`); the Job it owns is named from a hash of the inputs that affect behavior:

```
{migration.Name}-{hash}
```

where `hash` is a short hash (e.g. first 8 hex chars of FNV or SHA256) of the JDBC connection target, schema, image reference, and `Include`, and — only for the unused `ScriptSource != nil` fallback — the init container's image reference too. `LogicDbMigrationStatus.JobRef` always names the current hash's Job. A completed old-hash Job doesn't need active deletion — `ttlSecondsAfterFinished` GCs it; the reconciler just stops looking it up once the hash changes.

### Container Spec (defaults for all steps)

| Field | Value |
|---|---|
| `restartPolicy` | `Never` — retries via `backoffLimit`, not container restarts, so failures are visible as distinct Pod attempts |
| `backoffLimit` | Low (e.g. `2`) — a failed migration is unlikely to succeed on blind retry |
| `activeDeadlineSeconds` | Bounded (e.g. 300s) so a hung migration doesn't block reconciliation indefinitely |
| `ttlSecondsAfterFinished` | Set (e.g. 3600s) so finished Jobs are garbage-collected automatically |

### Failure Handling

A `Failed` Job is a hard stop, not a transient state — the reconciler does not recreate a Job with the same hash automatically. Recovery requires either the user fixing the underlying issue and letting the operator regenerate a new-hash Job on the next spec change, or manually deleting the failed Job (which the operator recreates with the same hash on the next reconcile, since nothing else changed). No new API surface (e.g. a retry annotation) is proposed for v1 — flagged in Open Questions if this proves insufficient in practice.

---

## Step 1: Runtime Schema Migration

**Scope:** `LogicPlatform.Spec.RuntimeDefaults.Persistence`. This is the JPA workflow-execution schema (`workflow_instance`, `task_info`, `cloud_event`, `completed_task`, `retried_task`), shared by every `LogicFlowRuntime` in the namespace that doesn't override its own `Persistence.PostgreSQL` (see Migration Ownership).

### Reconciliation Flow

`LogicPlatformReconciler` (the migration piece):

```
1. Fetch LogicPlatform
2. reconcileRuntimeMigration — only if RuntimeDefaults.Persistence != nil && DBMigrationStrategy == job
   - upserts LogicDbMigration{Name: "<platform.Name>-runtime-migration",
     Spec: {Component: "runtime", Include: ["runtime"], ...}} — Image left to its default
   - reads back the LogicDbMigration's Status; does not touch a Job directly
3. updateStatus — includes updateStatusRuntimeMigration (mirrors LogicDbMigration's
   MigrationComplete condition onto LogicPlatformStatus.RuntimeMigrationComplete)
```

`LogicFlowRuntimeReconciler` (the gating piece):

```
1. Fetch LogicFlowRuntime
2. List ConfigMaps
3. reconcileMigrationGate — looks up the LogicPlatform in rt.Namespace (see Migration Ownership)
   - 0 or Platform's RuntimeMigrationComplete == True: not gated, proceed
   - Platform's RuntimeMigrationComplete == False: gated, requeue short backoff
4. applyDeployment (SSA) — gated: if migration is required and not yet complete,
   skip applying the Deployment entirely (see "Blocking mechanism")
5. reconcilePodRBAC, reconcileLeases (unchanged, existing persistence-gated steps)
6. applyService (SSA)
7. updateStatus — includes updateStatusMigration (mirrors the Platform's
   RuntimeMigrationComplete condition onto LogicFlowRuntimeStatus.MigrationComplete),
   feeds MigrationComplete into DerivePhase
```

Migration is reconciled *before* the Deployment, inverting the existing lease ordering (which needs the Deployment's UID and so runs after it). The `LogicDbMigration`'s own owner reference points at the `LogicPlatform` CR directly (the Job's owner reference in turn points at the `LogicDbMigration` — see `LogicDbMigration` Controller). Creating/polling the Job itself is `LogicDbMigrationReconciler`'s job, not `LogicPlatformReconciler`'s or `LogicFlowRuntimeReconciler`'s — those two only read status.

### Blocking mechanism

Recommended: **skip `applyDeployment` entirely** on reconciles where migration is required and incomplete, rather than applying a Deployment scaled to 0. Scaling to 0 still creates the Deployment object and associated ReplicaSet churn, and risks racing with HPA (`adr/implementation/hpa-support.md`) fighting over replica count. Skipping the apply means no Deployment exists at all until migration succeeds — this changes "time to first pod" for Runtimes governed by a `job`-strategy Platform compared to `service`/`none` and should be called out in user docs.

Requeue while migration is in flight: short backoff (e.g. `RequeueAfter: 5 * time.Second`) rather than relying solely on watching the `LogicPlatform`.

### Container Spec (Step 1 specifics)

| Field | Value |
|---|---|
| Image | `db-migrator`'s well-known default — a distinct image from the runner's own serving image (see Migrator Application) |
| Env | Same as `persistenceEnvVars()` output (JDBC URL, credentials, TLS) plus `LOGIC_DB_MIGRATOR_INCLUDE=runtime` |

### Status

**`RuntimeMigrationComplete` condition (on `LogicPlatformStatus`)** — the source of truth, a mirror of the child `LogicDbMigration`'s own `MigrationComplete` condition:

| Status | Reason | When |
|---|---|---|
| `True` | `Ready` | `RuntimeDefaults.Persistence.DBMigrationStrategy != job`, or the child `LogicDbMigration` reports `Succeeded` |
| `False` | `MigrationJobRunning` | `LogicDbMigration.Status.Phase` is `Pending`/`Running` |
| `False` | `MigrationJobFailed` | `LogicDbMigration.Status.Phase` is `Failed` |

**`MigrationComplete` condition (on `LogicFlowRuntimeStatus`)** — a second-hop mirror of the Platform's `RuntimeMigrationComplete`, the gate `applyDeployment` actually checks. The detailed report (which streams applied, what version) lives on `LogicDbMigration.Status`, not duplicated at either level — `kubectl describe logicflowruntime` tells you *whether* migration is done, `kubectl describe logicdbmigration <platform.Name>-runtime-migration` tells you *what happened*.

New consts in `api/v1/status_types.go`, following the existing grouped style:

```go
ConditionMigrationComplete        = "MigrationComplete"
ConditionRuntimeMigrationComplete = "RuntimeMigrationComplete"
```
```go
ReasonMigrationJobRunning = "MigrationJobRunning"
ReasonMigrationJobFailed  = "MigrationJobFailed"
```

`DerivePhase` needs `MigrationComplete=False`/`MigrationJobFailed` to force `ApplicationPhaseFailed` (hard stop, unlike `DeploymentProgressing`'s "not ready yet"); `MigrationJobRunning` keeps phase at `Pending`.

### RBAC

`LogicPlatformReconciler`:

```go
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicdbmigrations,verbs=get;list;watch;create;update;patch
```

`LogicFlowRuntimeReconciler`:

```go
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicplatforms,verbs=get;list;watch
```

on `logicflowruntime_controller.go` — this reconciler only ever reads `LogicPlatform`; it never creates or writes a `LogicDbMigration`, and needs no `batch`/`jobs` RBAC at all (that belongs to `LogicDbMigrationReconciler`, see `LogicDbMigration` Controller RBAC below).

### Existing Dead Code

`utils/kubernetes/jobs.go` (`FindJob`, `FindJobs`, `JobHasFinished`) was ported from the upstream SonataFlow operator's own DB-migrator-Job feature and has zero callers today. `JobHasFinished`'s `(finished, succeeded bool)` signature fits `LogicDbMigrationReconciler`'s polling logic — reuse it there, but verify its `batchv1.JobComplete`/`JobFailed` condition handling matches current Kubernetes Job semantics before relying on it.

### `LogicDbMigration` Controller RBAC

```go
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicdbmigrations/status,verbs=get;update;patch
```

on `logicdbmigration_controller.go` — no `batch`/`jobs` or `pods` RBAC exists anywhere in the operator today. `pods: get;list` is new specifically for harvesting the termination-message report (see Migration Report).

### Files

| File | Change |
|---|---|
| `api/v1/persistence_types.go` | `DBMigrationStrategy` field type fix, enum marker, doc comment update |
| `api/v1/logicdbmigration_types.go` (new) | `LogicDbMigration`, `LogicDbMigrationSpec/Status`, `MigrationPhase`, `MigrationStreamStatus`, `LogicDbMigrationKind` |
| `api/v1/status_types.go` | `ConditionMigrationComplete`, `ConditionRuntimeMigrationComplete`, `ReasonMigrationJobRunning`, `ReasonMigrationJobFailed` |
| `api/v1/logicflowruntime_webhook.go` | Admission Warning when `Persistence.DBMigrationStrategy` is set directly on a `LogicFlowRuntime` |
| `internal/controller/logicplatform_controller.go` | `reconcileRuntimeMigration` (upserts `LogicDbMigration`), `updateStatusRuntimeMigration`, new RBAC marker |
| `internal/controller/logicflowruntime_controller.go` | `reconcileMigrationGate` (looks up `LogicPlatform`), `updateStatusMigration`, reordered `Reconcile`, new RBAC marker |
| `internal/controller/logicdbmigration_controller.go` (new) | `LogicDbMigrationReconciler` — builds/polls the Job, harvests the report, writes `LogicDbMigrationStatus` |
| `internal/controller/quarkus_config.go` | `persistenceEnvVars()` reads `DBMigrationStrategy` to force `database.generation=none` for `job`/`none` |
| `internal/controller/migrationjob_objects.go` (new) | Shared `migrationJobInputs`, `buildMigrationJob`, `migrationJobName`, hash logic — used only by `LogicDbMigrationReconciler` |
| `utils/kubernetes/jobs.go` | Reuse `FindJob`/`JobHasFinished`; verify condition-handling correctness |
| `config/crd/bases/*.yaml`, `config/rbac/role.yaml` | Regenerated via `make manifests` |
| `docs/` | User-facing documentation for all three strategies |

### Testing

**Unit:** Job-name hashing (stable for identical inputs, changes when JDBC URL/schema/image changes); `persistenceEnvVars()` emits `database.generation=none` for `job`/`none`, unset for `service`; termination-message JSON parsing (well-formed, malformed, missing).

**Envtest:** `LogicDbMigration` created when `LogicPlatform.Spec.RuntimeDefaults.Persistence.DBMigrationStrategy: job`; `LogicFlowRuntime` Deployment withheld until `LogicPlatformStatus.RuntimeMigrationComplete` is `True` (fake Job/Pod status via direct status-subresource updates, matching existing lease/RBAC test patterns, including a fake termination message for report-harvesting coverage); `MigrationComplete` transitions correctly on success/failure across `LogicDbMigration`, `LogicPlatformStatus`, and the mirrored `LogicFlowRuntimeStatus` condition; no `LogicDbMigration` for `service`/`none`; hash mismatch triggers a new Job rather than reusing the old one; zero `LogicPlatform` in namespace leaves the Runtime ungated; `Persistence.DBMigrationStrategy` set directly on a `LogicFlowRuntime` has no effect and triggers the admission Warning.

**Integration:** Extend `internal/controller/integration_test.go`: Platform with `job` strategy → Runtime has no Deployment yet → `LogicDbMigration`'s Job status faked to `Succeeded` with a termination message → reconcile both controllers → `LogicDbMigration.Status.Streams` populated, `LogicPlatformStatus.RuntimeMigrationComplete=True`, Runtime's Deployment now applied.

### Acceptance

`LogicPlatform` with `RuntimeDefaults.Persistence` + `DBMigrationStrategy: job` gets the runtime schema migrated via a Job before any governed `LogicFlowRuntime`'s Deployment is created; a failed migration blocks every such Runtime's startup with a clear condition; `service`/`none` behave as specified with zero Job involvement; a `LogicFlowRuntime` overriding its own `Persistence.DBMigrationStrategy` sees no effect on migration behavior.

---

## Step 2: + Data Index Migration

**Scope:** `LogicPlatform.Spec.DataIndex.Persistence`. **Blocked** on two things: `LogicPlatformReconciler` reconciling Data Index at all (currently a complete stub — `_ = logf.FromContext(ctx); return ctrl.Result{}, nil`; this ADR does not design that reconciliation — Deployment/Service for the Data Index app is ADR 001's EPIC 6, tracked separately), and `logic-apps` publishing `data-index-storage-migrations` as a Quarkus extension `db-migrator` can depend on at build time, which doesn't exist today (see Migrator Application, and Context). This section designs what plugs into both once they exist.

### What's different from Step 1

- **`DataIndexSpec.Persistence` is a value type, not a pointer** (`Persistence PersistenceOptionsSpec`, vs. `RuntimeDefaults.Persistence *PersistenceOptionsSpec`). The "has persistence" gate is therefore `DataIndex.Enabled && DataIndex.Persistence.PostgreSQL != nil`, not a nil-spec check — normalize to a pointer at the `LogicDbMigrationSpec` call site so `LogicDbMigrationReconciler` doesn't need a second code path.
- **Same migrator image and mode as Step 1 (Bundled) — not the main Data Index app image.** `data-index-storage-migrations` is a plain Flyway-core library today (no Quarkus extension, no Dockerfile), pulled into `data-index-service-postgresql` only inside a `dev-flyway` Maven profile with production Flyway explicitly disabled — building a migrate-only mode into that production app isn't the design here (see Migrator Application for why). Instead, `logic-apps` packages it as a Quarkus extension (working name `data-index-db-migration`, exact naming TBD) that `db-migrator` takes a compile-time dependency on, the same way it already does on `quarkus-flow-db-migration-runtime`/`-quartz` — `LogicDbMigrationSpec.Include: ["data-index"]` selects it at invocation time, no `ScriptSource`/init container involved. Separately, `data-index-ingestion-kafka-service` depends on the same migrations module only at `scope=test`, so it carries no production Flyway wiring — whether it owns any schema of its own is unresolved and treated as out of scope for Step 2.
- **No ordering dependency with Step 1's migration.** The runtime schema and Data Index each get their own `LogicDbMigration` object under the same `LogicPlatform`, reconciled independently by the same shared `LogicDbMigrationReconciler` — and (per the existing `databaseSchema` field's own doc comment: "use a different databaseSchema than runtimes") typically target different schemas even when sharing a Postgres server. Neither migration blocks the other — this is worth stating explicitly since it's a natural question once both exist.

### Reconciliation Flow (once Data Index reconciliation exists)

```
... (Data Index Deployment/Service reconciliation from EPIC 6) ...
reconcileDataIndexMigration — only if DataIndex.Enabled && DataIndex.Persistence.PostgreSQL != nil
                               && DataIndex.Persistence.DBMigrationStrategy == job
  - upserts LogicDbMigration{Name: "<platform.Name>-data-index-migration",
    Spec: {Component: "data-index", Include: ["data-index"], ...}} — Image left to its default
applyDataIndexDeployment — gated the same way Step 1 gates applyDeployment, off the LogicDbMigration's status
updateStatus — includes updateStatusDataIndexMigration (mirrors the child's MigrationComplete condition)
```

### Status: `DataIndexMigrationComplete` condition (on `LogicPlatformStatus`)

Same True/False/reason shape as Step 1's `RuntimeMigrationComplete` — a mirror of the child `LogicDbMigration`'s condition, namespaced to Data Index since a `LogicPlatform` hosts more than one migratable stream. The report itself (streams, applied version) lives on `LogicDbMigration.Status`, same as Step 1:

```go
ConditionDataIndexMigrationComplete = "DataIndexMigrationComplete"
```

Reuses `ReasonMigrationJobRunning`/`ReasonMigrationJobFailed` from Step 1 rather than duplicating reason constants.

### Files

| File | Change |
|---|---|
| `api/v1/status_types.go` | `ConditionDataIndexMigrationComplete` |
| `internal/controller/logicplatform_controller.go` | `reconcileDataIndexMigration` (upserts `LogicDbMigration`), `updateStatusDataIndexMigration` — layered onto whatever EPIC 6 lands for base reconciliation |
| `internal/controller/logicdbmigration_controller.go` | No change — reused as-is from Step 1, since it's already generic over `Component` |
| `internal/controller/migrationjob_objects.go` | No change — reused as-is from Step 1 |

### Testing

Same shape as Step 1's envtest/integration coverage, applied to the Data Index stream instead: Job created/withheld/status-driven, plus one integration test confirming Data Index and runtime migrations proceed independently (fail one, confirm the other is unaffected).

### Acceptance

`LogicPlatform` with Data Index enabled + persistence + `DBMigrationStrategy: job` gets its schema migrated via a Job before the Data Index Deployment is created, independent of the runtime-schema migration.

---

## Step 3: + Quartz Migration

**Scope:** Extends Step 1's runtime-schema Job to also apply the Quartz scheduler schema stream when Quartz scheduling is enabled for the Platform's governed runtimes. **Blocked** on an API gap this ADR does not resolve: the operator has zero notion of Quartz today — no `Quartz`/`Scheduler` references anywhere in the Go code. Before this step can be built, a Platform-level signal expressing "runtimes governed by this Platform use Quartz" needs to exist (shape, JDBC-target semantics, and whether Quartz always shares `RuntimeDefaults.Persistence.PostgreSQL` or can point elsewhere are all open design questions for a separate, future ADR).

### What's different from Step 1

Given that prerequisite API exists:

- **No new Job shape, no new image, no new resolution logic.** Because `db-migrator` always bundles both the runtime and Quartz migration scripts (see Migrator Application — bundling both costs nothing, they're small SQL files), Step 3 is purely a matter of what value the operator puts in `LogicDbMigrationSpec.Include`: `["runtime","quartz"]` instead of `["runtime"]`, on the *same* `<platform.Name>-runtime-migration` `LogicDbMigration` object Step 1 already creates — not a second one. `DefaultRunnerImage` and everything else about the Job's shape are unaffected.
- **Distinguishing which stream failed is not a special case.** `LogicDbMigrationStatus.Streams` (see Migration Report) already reports one entry per stream applied in a Job — with `Include: ["runtime","quartz"]`, that's naturally two entries, and a partial failure shows up as one `Streams` entry present and the `error` field naming the other. No dedicated `ReasonMigrationJobFailedQuartz` or free-text convention is needed. It still depends on the migrator app's exit reporting being granular enough to tell the two streams apart — itself one of the companion ADR's open questions, not yet resolved there.

### Files

| File | Change |
|---|---|
| `internal/controller/logicplatform_controller.go` | `reconcileRuntimeMigration` sets `LogicDbMigrationSpec.Include: ["runtime","quartz"]` instead of `["runtime"]` when Quartz scheduling is configured for the Platform |
| Wherever the new Scheduler API field lands (not designed here) | New field + validation |

### Testing

Envtest: Platform with Quartz scheduling enabled + `job` strategy sets `LogicDbMigrationSpec.Include: ["runtime","quartz"]` (same image, same shape as Step 1 otherwise), and a faked two-entry termination message produces two `LogicDbMigrationStatus.Streams` entries. Full end-to-end migration behavior (does the migrator actually apply both streams correctly) is not testable in envtest and belongs to quarkus-flow's own test suite.

### Acceptance

`LogicPlatform` with runtime persistence + Quartz scheduling + `DBMigrationStrategy: job` runs a single migration Job that applies both the JPA runtime schema and the Quartz schema before any governed Runtime's Deployment is created.

---

## Open Questions

- **All steps:** Whether a validating webhook should reject a second `LogicPlatform` per namespace, to remove the ambiguity in how `LogicFlowRuntimeReconciler` resolves "its" Platform (see Migration Ownership) — this ADR assumes the convention but doesn't enforce it.
- **Step 1/3:** Exact env var names (`LOGIC_DB_MIGRATOR_INCLUDE` and any others) and exit-code contract on failure (partial migration vs. connection failure vs. checksum mismatch) — depends on the companion quarkus-flow ADR's implementation.
- **All steps:** `db-migrator` module creation and scaffolding within `logic-apps` (CI wiring for a new per-module image build, container registry/namespace, base image, initial Maven/Quarkus project shape) is a prerequisite this ADR assumes but doesn't perform — not designed here.
- **All steps:** Versioning/compatibility policy between `db-migrator` releases and the `quarkus-flow-db-migration-runtime`/`-quartz` artifact versions it bundles — e.g. does `db-migrator` pin exact versions, a range, or always-latest, and who's responsible for bumping it when `quarkus-flow` cuts a new script release. Also whether `db-migrator` versions/releases independently within `logic-apps` (like its other per-module images) or piggybacks on a broader `logic-apps` release.
- **Step 2:** Exact shape and name of the `logic-apps`-published Data Index migration extension (working name `data-index-db-migration` in this doc), and whether `data-index-ingestion-kafka-service` needs its own migration path given it currently has no production Flyway wiring at all — both are `logic-apps`-side work this ADR doesn't design.
- **Step 3:** The Quartz scheduler-configuration API itself is undesigned; this step cannot start until that exists.
- **All steps:** The companion quarkus-flow ADR (`adr/2026-08-25-db-migration-extension-design.md`) still describes two separate migrator apps living inside `quarkus-flow` and, before that, "reuse the existing runner image with a migrate-only property" — both superseded by this document's Migrator Application section. That ADR needs a follow-up update before the two are consistent; not done as part of this change.
- **All steps:** Job failure retry UX — is deleting the failed Job manually (letting the operator recreate it) sufficient, or does this need a forced-retry annotation? Leaning toward the former (no new API surface).
- **All steps:** Whether `PostgreSQLServiceOptions.DatabaseSchema` needs anything beyond what's already in the JDBC URL (`currentSchema=`) for migration Jobs specifically — likely no, but worth an explicit check once the migrator app is prototyped.
- **All steps:** Exact JSON schema and versioning of the migrator's termination-message report (`streams[].name/appliedVersion/appliedCount`, error shape) — lives in the migrator's own contract and needs to stay in lockstep with `LogicDbMigrationStatus.Streams`'s parsing (see Migration Report).
- **All steps:** Whether `LogicDbMigrationStatus` should retain any history beyond the current Job's report (e.g. previous `Streams` snapshot on a hash change) or stay strictly "current state only," leaving history to the underlying `batchv1.Job`s within their TTL window — this design assumes the latter; revisit if the audit-trail need turns out to be longer-lived than that.
- **All steps:** Whether `LogicDbMigration` needs `kubectl` printer columns (`Phase`, `Component`, age) for it to be a good day-2 UX on its own, beyond just existing as a status-bearing object — not designed here, but likely a small addition alongside `config/crd/bases/*.yaml` generation.
