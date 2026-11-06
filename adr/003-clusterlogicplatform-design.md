# ADR 003: ClusterLogicPlatform — Single-Platform-Per-Cluster Design

**Date**: 2026-11-05
**Status**: Proposed
**Scope**: Supersedes the `central`/`remote` multi-tenant direction in
[issue #12](https://github.com/kubesmarts/logic-operator/issues/12). Introduces a
cluster-scoped singleton platform and repurposes runtime/persistence configuration.

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Context](#context)
3. [Goals, Non-Goals, Anti-Goals](#goals-non-goals-anti-goals)
4. [Decision Overview](#decision-overview)
5. [CRD Changes](#crd-changes)
6. [Persistence Model](#persistence-model)
7. [Quartz Scheduler Safety](#quartz-scheduler-safety)
8. [Database Migration](#database-migration)
9. [Cross-Namespace RBAC](#cross-namespace-rbac)
10. [Migration From `LogicPlatform`](#migration-from-logicplatform)
11. [Spin-Off Work](#spin-off-work)
12. [Left To The Builder](#left-to-the-builder)

---

## Executive Summary

Issue #12 originally proposed multi-tenant federation: a `central` platform hosting
a shared Data Index, with `remote` platforms resolving it via `dataIndexRef` and
cross-namespace RBAC. This ADR **drops multi-tenancy for v1** and replaces it with a
simpler, operationally cleaner model:

- **Exactly one platform per cluster**, expressed as a new **cluster-scoped singleton
  CRD, `ClusterLogicPlatform`**, which deploys a single Data Index, a single Workflow
  Gateway, and a single Vector DaemonSet into an elected `targetNamespace`.
- The existing **namespaced `LogicPlatform` CRD is removed** (hard replace; pre-GA).
- **Runtimes** are deployed by app teams into their own namespaces across the cluster
  and all report up to the one platform — giving a single pane of glass for the
  Backstage Orchestrator plugin and a single OIDC/RBAC choke point.
- **Three independent, bring-your-own PostgreSQL datastores** (Data Index, runtime,
  quartz), each pointable at the same or different physical instances.

Data isolation between teams is handled by **read-time RBAC at the Data Index**, not
by physical database separation. Physical isolation, when a customer truly needs it,
is achieved by running **another cluster/cell** (another `ClusterLogicPlatform`), not
by sharding databases inside one platform.

## Context

Today (`api/v1`, product "Alpha2"):

- `LogicPlatform` is **namespaced** (`scope=Namespaced`), documented as "only one per
  namespace," and deploys Data Index + Gateway + Vector in its own namespace.
- There is **no `mode` field**; the `central`/`remote` design in #12 was never built.
- `LogicPlatform.spec.runtimeDefaults` exists only as a **stub** "for future
  platform-level" use.
- `LogicFlowRuntime.spec.persistence` lets each runtime define its own database.
- Persistence types (`PersistenceOptionsSpec`, `PersistencePostgreSQL`) already model
  `secretRef`, `serviceRef`, `databaseName`, `databaseSchema`, TLS, and a
  `dbMigrationStrategy` enum (`service`/`job`/`none`). None of the migration
  strategies are acted on yet.
- There is **no jobs service / quartz code** in the operator; quartz is a concern of
  the runtime image (`quarkus-flow`) and of the upcoming DBMigrator.

Why pivot now: multi-tenant federation adds `dataIndexRef` resolution, per-tenant Data
Index instances, and cross-namespace plumbing that make the **first** release harder to
build and far harder for users to operate. Scattering Data Index/Gateway instances also
breaks the Backstage Orchestrator plugin, which expects exactly one Data Index/Gateway.
A single entrypoint simplifies the Gateway, OIDC/RBAC, and the Backstage integration.

### Key facts established during design

- **The cross-namespace plumbing is inherent, not optional.** With the platform in one
  elected namespace and runtimes spread across the cluster, each runtime's Vector still
  reaches the Data Index Service, its Secret, and the databases in `targetNamespace`.
  Collapsing to one-per-cluster removes `dataIndexRef` *resolution* and the "which
  central do I bind to" validation, but not the RBAC plumbing itself.
- **The industry norm is a shared store with logical isolation**, not DB-per-tenant.
  Temporal (namespaces within one store, scale-out via more clusters), Camunda 7 (one
  DB per engine), and Kogito/SonataFlow (one DB, logical separation) all follow this.
  The answer to "I need physical isolation" is another cell/cluster.
- **Runtime and quartz are already independent datasources** in `quarkus-flow`
  (`flow-runtime` via Hibernate ORM; `flow-quartz` via Quartz `jdbc-cmt`), each with
  its own Flyway history. There is **no XA/JTA** across them, so splitting them into
  separate physical databases costs nothing transactionally — there was never a shared
  transaction to lose. Residual coupling is eventual (an orphaned trigger whose
  workflow did not commit), which `QuartzJob` already tolerates by re-resolving the
  workflow definition at fire time.

## Goals, Non-Goals, Anti-Goals

### Goals
- One Data Index, one Gateway, one Vector DaemonSet per cluster — a single pane of
  glass for Backstage and a single OIDC/RBAC choke point.
- Cluster admins elect where the platform runs; app teams deploy runtimes anywhere and
  they auto-associate with the one platform.
- Three independently-configurable, bring-your-own PostgreSQL datastores, collapsible
  onto one instance for the simple/demo case.
- Least-privilege database access: DDL only for migration, DML only for operation.
- A clean, documented upgrade path off the current `LogicPlatform`.

### Non-Goals (v1; treat as permanent unless re-opened)
- **Multi-tenant federation** (`central`/`remote`/`dataIndexRef`) — the original #12
  scope.
- **Intra-platform runtime-DB sharding** / per-namespace runtime persistence defaults.
  Isolation is served by running another cell, not by sharding within a platform.
- **Operator-managed/bundled PostgreSQL + HA/backup.** Persistence is bring-your-own;
  the customer owns availability and backups.
- **A namespaced `LogicPlatform` variant.** Likely never re-introduced; future
  isolation is another `ClusterLogicPlatform` cell.

### Anti-Goals (would make this a failure even if it "works")
- Any design that results in more than one Data Index/Gateway visible to Backstage.
- Allowing two different runtimes to silently share a Quartz `SCHED_NAME` (cross-tenant
  trigger/lock corruption).
- Handing operational (runtime) pods DDL-capable database credentials.
- A breaking change to the runtime→platform association that cannot later admit a
  second platform kind without an API break.

## Decision Overview

| # | Decision |
|---|----------|
| 1 | Replace namespaced `LogicPlatform` with a **cluster-scoped singleton `ClusterLogicPlatform`** carrying `spec.targetNamespace`. |
| 2 | Platform deploys exactly one Data Index (H/V scalable), one Gateway, one Vector DaemonSet into `targetNamespace`. |
| 3 | Add `LogicFlowRuntime.spec.platformRef {name, kind}` (kind defaults to `ClusterLogicPlatform`) as the forward-compatible discovery seam. |
| 4 | Three independent BYO datastores: Data Index, runtime, quartz — each a `PersistenceOptionsSpec`, collapsible onto one instance via `databaseSchema`. |
| 5 | Repurpose per-runtime DB config to a **choice**: use the platform runtime DB, or **no DB (ephemeral → in-memory scheduler, no quartz)**. Remove raw DB connection config from `LogicFlowRuntime`. |
| 6 | Two-credential least-privilege model per datastore: a **DDL** `secretRef` used only by the migration Job, a **DML-only** `secretRef` used by operational pods. |
| 7 | One **DBMigrator Job per platform** (`dbMigrationStrategy=job`) runs Flyway against whichever distinct datastores are configured. Future `spec.dbMigrator` field. |
| 8 | Operator enforces Quartz safety: deterministic unique/stable `instance-name`, `clustered=true` for multi-replica runtimes, `instance-id=AUTO`. |
| 9 | Cross-namespace RBAC: Role/RoleBinding so app-namespace runtimes/Vector reach the Data Index Service + Secret in `targetNamespace`. |

## CRD Changes

### New: `ClusterLogicPlatform` (cluster-scoped, singleton)

- `+kubebuilder:resource:scope=Cluster`.
- **Singleton** enforced by a validating webhook (reject creation of a second one).
- `spec.targetNamespace` (required): where Data Index, Gateway, and Vector are
  deployed. A cluster-scoped object has no namespace of its own; this is its analogue
  to cert-manager's "cluster resource namespace."
- Reuses the existing component specs (`DataIndexSpec`, gateway, ingress) from today's
  `LogicPlatform`, relocated/renamed as needed.
- `spec.runtime.db` (see below): **the** runtime datastore for the cluster — not a
  "default," the single definition.
- `spec.dataIndex.persistence` and `spec.quartz` persistence (see below).
- Future: `spec.dbMigrator` for migration tuning (image, resources, timeouts).

### Removed: namespaced `LogicPlatform`

Deleted along with its generated CRD, webhook registration, and the four RBAC roles
(`logicplatform_editor_role`, `logicplatform_admin_role`, `logicplatform_viewer_role`,
`workflow_gateway_clusterrole`), samples, and chart template. See
[Migration From `LogicPlatform`](#migration-from-logicplatform).

### Changed: `LogicFlowRuntime`

- **Add** `spec.platformRef {name, kind}`, `kind` defaulting to `ClusterLogicPlatform`.
  This is the one seam that must exist from day one so a future platform kind (a cell,
  or a hypothetical namespaced variant) can be referenced without an API break.
- **Replace** `spec.persistence` (raw DB config) with a **persistence mode**:
  - *use platform DB* — inherit the platform's `spec.runtime.db` (and quartz).
  - *no DB (ephemeral)* — select the in-memory scheduler (`scheduler/memory`); Quartz
    is not used, so there is **no** quartz datastore or `SCHED_NAME` concern. Suitable
    for minimal/stateless deployments.
- The persistence choice and the scheduler backend are **one decision**: ephemeral ⇒
  in-memory scheduler; DB-backed ⇒ Quartz `jdbc-cmt` + the invariants below.
- v1 is **binary** (ephemeral/in-memory vs platform-DB/quartz) — the only two scheduler
  modules that exist today. The field should be shaped so a third scheduler could be
  added later without an API break, but no third option is modeled now. *(Resolved
  during builder-check.)*

### Runtime→platform discovery

- When `platformRef` is **omitted**, the runtime auto-associates with the cluster's
  single `ClusterLogicPlatform` (list; take the one that exists). This is unambiguous
  because the singleton webhook rejects a second platform.
- When `platformRef.name` is **set**, validate it resolves to an existing platform.
- *(Resolved during builder-check: follows directly from the singleton decision.)*

## Persistence Model

Three **logical datastores**, each an independent `PersistenceOptionsSpec`:

| Datastore | Fed by | Workload | Default placement |
|-----------|--------|----------|-------------------|
| Data Index | Vector/events (async, eventual) | Read-heavy GraphQL | **Separate instance** (read isolation) |
| Runtime | Workflow state (sync writes) | Operational hot path | Operator/customer choice |
| Quartz | Scheduler (`jdbc-cmt`) | Timer/trigger polling, `QRTZ_LOCKS` contention | Operator/customer choice |

- **Bring-your-own**: the customer runs PostgreSQL and supplies connection details and
  secrets. The operator never provisions a database or owns HA/backup.
- Each datastore can target the same physical instance (distinct `databaseSchema`) or a
  separate instance. Reuse the existing `serviceRef`/`secretRef`/`databaseName`/
  `databaseSchema`/`tls` fields.
- **Default recommendation**: Data Index on its own instance (read-load isolation is
  the highest-value split); runtime and quartz may share an instance or be split — the
  CRD allows both. (Builder picks the zero-config/demo default — see
  [Left To The Builder](#left-to-the-builder).)
- **Separating runtime and quartz is transactionally free** (no XA today).

### Two-credential least-privilege model

Per datastore, the design distinguishes:

- A **DDL credential** (`secretRef` on the migration path) — can create/alter schema.
  Used **only** by the DBMigrator Job.
- A **DML-only credential** (`secretRef` on the operational connection) — no DDL
  rights. Used by Data Index, runtime, and quartz pods at runtime.

**Contract**: the customer provides empty databases and a DDL-capable user for
migration, plus a restricted DML user for operation. The operator wires each to the
right workload.

**Credentials are per-datastore, not platform-level.** Because the three datastores
may live on three different physical instances (each BYO), a single platform-wide DDL
secret cannot span them. Each datastore config therefore carries its own DDL `secretRef`
(migration) and DML `secretRef` (operational). A customer who collocates all three on
one instance may simply reference the same secret from each — but the field structure
is per-store. *(Resolved during builder-check: forced by the independent-instance
decision.)*

## Quartz Scheduler Safety

Quartz tables are partitioned by `SCHED_NAME` (leading column of every PK), and runtime
tables by `application_id`, so multiple runtimes can **safely share** one runtime and/or
quartz database — **provided `SCHED_NAME` discipline holds**. Today `quarkus-flow` sets
neither `instance-name` nor `clustered`, so every deployment defaults to the shared
constant `QuarkusQuartzScheduler` — unsafe for a shared quartz store.

The operator must therefore generate, for each DB-backed runtime:

- `quarkus.quartz.instance-name = f(runtime identity)` — **deterministic, unique per
  `LogicFlowRuntime`, identical across its replicas, and stable across restarts/
  upgrades** (changing it orphans in-flight triggers). Natural basis:
  `quarkus.application.name`.
- `quarkus.quartz.clustered = true` when a runtime has more than one replica (so
  replicas cluster correctly via `QRTZ_LOCKS` / `QRTZ_SCHEDULER_STATE`).
- `quarkus.quartz.instance-id = AUTO` for per-pod identity within that cluster.

This invariant is **the** thing that makes a shared quartz database safe.

## Database Migration

- One **DBMigrator Job per platform** (`dbMigrationStrategy=job`), owned by
  `ClusterLogicPlatform`, running in `targetNamespace` (co-located with the secrets and,
  usually, the databases it migrates). *(Resolved during builder-check.)*
- Runs Flyway against whichever **distinct** datastores are configured (1–3 connection
  targets: Data Index, runtime, quartz), using the **DDL credential** for each.
- Flyway histories are already separate per store (`flyway_flow_runtime_history`,
  `flyway_flow_quartz_history`, and the Data Index equivalent).
- **Image delivery**: the operator ships a prebuilt migrator image on **Quay**,
  tagged per release so the Flyway scripts always match the `quarkus-flow` version
  (consistent with the "align Alpha2 releases with Quay image tags" work). `kind`/demo
  works out of the box with no image to build.
- `spec.dbMigrator.image` (optional) **overrides** the default for power users who need
  custom migrations; when empty it defaults to
  `quay.io/kubesmarts/logic-db-migrator:<release-tag>`.
- Future `spec.dbMigrator` also tunes resources/timeouts/baseline handling (the
  populated-database upgrade case needs a Flyway baseline).

## Cross-Namespace RBAC

This is the surviving slice of #12. With runtimes in app namespaces and the platform in
`targetNamespace`:

- A **Role** in `targetNamespace` granting read on the Data Index connection Secret.
- A **RoleBinding** for each runtime's ServiceAccount (or Vector's SA) so it can read
  that Secret and reach the Data Index Service.
- The operator reconciles these as runtimes come and go. It manages the ServiceAccount
  it binds (creates/owns it rather than assuming pre-existence), so binding never races
  a missing SA. *(Resolved during builder-check; dedicated-vs-shared SA left to the
  builder.)*

## Migration From `LogicPlatform`

Changing a CRD's scope is not an in-place conversion: namespaced → cluster-scoped means
a **new CRD** and users must **delete and recreate**. Because the product is pre-GA,
a hard replace is acceptable, but it must be documented:

- Provide a migration guide: remove the old `LogicPlatform`, install
  `ClusterLogicPlatform` with `targetNamespace` set to the previous platform namespace,
  re-point runtimes (now via `platformRef`).
- Update the released Helm chart (`charts/logic-operator-stack/`, `dist/helm/`) and the
  samples (`config/samples/...`).
- Call out impact on the Backstage integration and the demo-day flow.

## Spin-Off Work

1. **quarkus-flow** (`../quarkiverse/quarkus-flow`): default
   `quarkus.quartz.instance-name` from `quarkus.application.name` and handle
   `clustered`/`instance-id` so the runtime is correct-by-construction even without the
   operator. (Operator still overrides via properties.)
2. **logic-operator**: feed/override the quartz properties per runtime from the
   reconciler.

## Left To The Builder

- Exact Go type layout and field names for `ClusterLogicPlatform` and the three
  persistence blocks (reuse `PersistenceOptionsSpec` shapes).
- The zero-config/demo default topology (collapse all three datastores onto one sample
  instance vs. keep Data Index separate) and the sample Postgres manifest + secret for
  the `kind`/demo path.
- Webhook mechanics for singleton enforcement and `platformRef` validation.
- Printer columns, status conditions, and finalizer/GC details (cluster-scoped owner →
  namespaced dependents is permitted for GC).
- The precise `f(runtime identity)` function for `SCHED_NAME` (recommended basis:
  `{namespace}/{name}` — human-readable and stable; a rename is a new runtime identity,
  which is acceptable because it starts a fresh scheduler rather than colliding).
- Migration-guide wording and chart changes.
