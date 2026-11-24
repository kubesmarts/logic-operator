# Database Schema Migration Strategy Design

**Date:** 2026-09-29 (revised 2026-11-19 for `ClusterLogicPlatform`)
**Status:** Implemented — see [Known Gaps](#known-gaps)

## Context

Quarkus Flow runtimes persist workflow state and Quartz scheduler tables to PostgreSQL, and Data Index persists its index. Each datastore — `ClusterLogicPlatform.spec.runtime.persistence.runtime`, `.quartz`, and `spec.dataIndex.persistence` — is a `DatastoreSpec` with a `dbMigrationStrategy`:

| Strategy | Behavior |
|----------|----------|
| `service` (default) | The workload manages its own schema at startup (Hibernate for runtimes, Flyway for Data Index). |
| `job` | The operator runs a `logic-db-migrator` Job before the workload rolls out; the workload does not run DDL. |
| `none` | Schema is externally managed; the operator does nothing and the workload does not run DDL. |

`job` exists because `service` migrations run with the application's credentials and race across replicas (every pod attempting DDL on boot). It gives a single, auditable, least-privilege migration step gated in front of rollout.

## Decisions

Following review of [#31](https://github.com/kubesmarts/logic-operator/pull/31):

- **No new CRD.** Migration is a platform component configured by `ClusterLogicPlatform.spec.dbMigrator`, like `dataIndex` or `gateway`. Its status lives in `ClusterLogicPlatform.status.dbMigrator`.
- **Reuse `ApplicationSpec`.** `spec.dbMigrator.application` configures image, resources and pod template; there is no dedicated image field.
- **Strategy is platform-only.** `LogicFlowRuntime` has no persistence of its own; it inherits the platform's datastores and their strategy. There is no per-runtime Job.
- **The migrator lives in `logic-apps`** and is published as `quay.io/kubesmarts/logic-db-migrator`. The default tag comes from `DB_MIGRATOR_VERSION` in `versions.mk`, injected at build time into `version.GetDBMigratorVersion()`.

## Component Flow

`dbMigratorComponent` (`internal/controller/clusterplatform_dbmigrator.go`) is one of the `PlatformComponent`s run by `ClusterLogicPlatformReconciler`.

```
ClusterLogicPlatform (spec.runtime.persistence.*.dbMigrationStrategy=job)
  └── batch/v1 Job "{platform}-db-migrator-{datastore}-{hash}"   (in targetNamespace)
        └── Pod (db-migrator container)
```

`migrationTargets` decides which datastores need a Job. The migrator takes a single datasource, so there is one Job per datastore. The `quartz` stream runs only when the `quartz` datastore opts into `job`; when it has no `postgresql`, it inherits the runtime connection and `migrationSecretRef` (matching the runner, which falls back to the runtime datasource for Quartz):

| Configuration | Jobs |
|---------------|------|
| `runtime` = `job`, `quartz` not `job` | `runtime` Job (`runtime`); Quartz tables are not migrated by the operator |
| `runtime` = `job`, `quartz` = `job` | `runtime` Job (`runtime`) and `quartz` Job (`quartz`) |
| only `quartz` = `job` | `quartz` Job (`quartz`) |
| `dataIndex.persistence` = `job` | `data-index` Job (`data-index`), independent of the runtime ones |

**Reconcile** creates each missing Job and deletes migration Jobs of this platform that are no longer desired (strategy switched away, or configuration changed). A Job whose status already records `Succeeded` under the same name is not recreated after its TTL removes it; a failed Job reruns once deleted (manually or by TTL).

**UpdateStatus** fills `status.dbMigrator.migrations[]` (`datastore`, `jobRef`, `phase`, `message`, `streams`) and the `DBMigration` condition:

- every Job succeeded → `True/Ready`
- any Job failed → `False/MigrationJobFailed` (phase `Failed`)
- otherwise → `False/MigrationJobRunning`
- no datastore uses `job` → condition and `status.dbMigrator` removed

The controller `Owns(&batchv1.Job{})`, so Job transitions trigger a reconcile; there is no polling.

### Job naming

Jobs are immutable, so the name hashes (FNV-32a) the whole rendered pod template: image, env (connection, secret name/key coordinates, included streams) and every `ApplicationSpec` override. Any behavioral change spawns a new Job. Secret values never reach the template, so rotating a secret's value does not rerun the migration. The base name is truncated to keep the full name within 63 characters (the `job-name` Pod label limit).

### Job specification

| Field | Value | Why |
|-------|-------|-----|
| `backoffLimit` | 2 | A couple of retries for transient connection failures |
| `activeDeadlineSeconds` | 300 | A hung migration must not block rollout indefinitely |
| `ttlSecondsAfterFinished` | 3600 | Finished Jobs self-clean; Pods stay long enough to debug |
| `restartPolicy` | `Never` (forced) | Retries are Job-level, so the termination message is never overwritten mid-attempt |
| security context | restricted pod/container helpers | Same restricted-PSS defaults as other components |

Owner reference: the cluster-scoped `ClusterLogicPlatform` (a namespaced dependent of a cluster-scoped owner is valid).

### Env contract

The container receives:

- `QUARKUS_DATASOURCE_USERNAME` / `QUARKUS_DATASOURCE_PASSWORD` from the datastore's **DDL** credential (`migrationSecretRef`, falling back to `postgresql.secretRef`), read from `targetNamespace`.
- `QUARKUS_DATASOURCE_JDBC_URL` built the same way as for runtimes (schema via `currentSchema`, TLS via `sslmode`).
- `LOGIC_APPS_MIGRATOR_INCLUDE` — comma-joined streams to apply (`runtime`, `quartz`, `data-index`).

### Report harvesting

The migrator writes a JSON report to its termination message:

```json
{"streams":[{"name":"runtime","appliedVersion":"1.0.0","appliedCount":1}],"error":"optional failure detail"}
```

`harvestMigrationReport` lists the Job's Pods by `job-name`, takes the most recently created one (retries can produce several) and parses the `db-migrator` container's termination message. An empty message (e.g. OOMKilled) parses to an empty report. `lastAppliedAt` uses the Job's `completionTime`, so repeated reconciles keep the status stable.

## Gating

Both gates use `pendingMigration`, which reads `status.dbMigrator` and only accepts a `Succeeded` entry whose `jobRef` matches the Job the current spec would build — a success recorded for an outdated Job does not count. Each workload waits only for its own datastores, so a Data Index migration failure never blocks runtimes and vice versa. While gated, the Deployment is neither created nor updated (an existing one keeps running).

### Runtimes

`LogicFlowRuntimeReconciler.setMigrationCondition` runs after persistence is resolved:

- Ephemeral runtime, or neither `runtime` nor `quartz` uses `job` → no gate; `DBMigration` condition removed.
- Otherwise the runtime's `DBMigration` condition is `True/Ready`, `False/MigrationJobRunning`, or `False/MigrationJobFailed` (which drives the runtime phase to `Failed` through `DerivePhase`).

Runtimes already watch `ClusterLogicPlatform` without a predicate, so a platform status change re-reconciles them.

### Data Index

`dataIndexComponent` skips its Deployment until the `data-index` migration succeeded. Components run in order and status is written once per reconcile, so the rollout happens on the reconcile triggered by that status write.

## Disabling Workload DDL

- Runtimes: `persistenceQuarkusEnvVars` sets `QUARKUS_HIBERNATE_ORM_SCHEMA_MANAGEMENT_STRATEGY=none` when the runtime datastore's strategy is `job` or `none`. It is not emitted for the Quartz datasource or the migrator container; instead, `WithQuartzMigrationEnvVars` sets `QUARKUS_FLYWAY_FLOW_QUARTZ_MIGRATE_AT_START=false` when the Quartz strategy is `job` or `none`, since the runner's standard profile migrates Quartz at startup.
- Data Index: `WithFlywayPersistenceVars` sets `QUARKUS_FLYWAY_MIGRATE_AT_START=false` (and drops the baseline settings) when its strategy is `job` or `none`.

## CRD Size

Embedding another `ApplicationSpec` pushed the `ClusterLogicPlatform` CRD to ~1.66 MB, above etcd's default 1.5 MiB request limit. CRDs are now generated with `crd:maxDescLen=0` (~350 KB); field documentation remains in the Go types and the Antora CRD reference, but `kubectl explain` no longer shows descriptions.

## Files

| File | Role |
|------|------|
| `api/v1/clusterlogicplatform_types.go` | `DBMigratorSpec`, `DBMigratorStatus`, `DatastoreMigrationStatus`, `MigrationStreamStatus`, `MigrationPhase` |
| `api/v1/persistence_types.go` | `DatastoreSpec.DBMigrationStrategy` enum |
| `api/v1/status_types.go` | `ReasonMigrationJobRunning`, `ReasonMigrationJobFailed`; `DerivePhase` treats migration failure as `Failed` |
| `api/v1/images.go`, `api/version/version.go` | `DefaultDBMigratorImage()`, `GetDBMigratorVersion()` |
| `internal/controller/migrationjob_objects.go` | `migrationTargets`, env assembly, content-addressed Job builder |
| `internal/controller/migration_report.go` | Termination-message parsing |
| `internal/controller/clusterplatform_dbmigrator.go` | `dbMigratorComponent` — Job lifecycle, report harvesting, status |
| `internal/controller/logicflowruntime_controller.go` | `setMigrationCondition`, Deployment gating |
| `internal/controller/clusterplatform_dataindex.go` | Data Index Deployment gating |
| `internal/controller/quarkus_config.go` | Hibernate schema-management env var |
| `internal/controller/platform_shared_objects.go` | Data Index Flyway env vars |

## Testing

| Test | Coverage |
|------|----------|
| `migration_report_test.go` | Empty, well-formed, error and malformed reports |
| `migrationjob_objects_test.go` | Target matrix (runtime/quartz/data-index), DDL credential, Job defaults, `ApplicationSpec` overrides, name stability and change matrix, truncation |
| `clusterplatform_dbmigrator_test.go` | No-op without `job`; success with streams; TTL-removed success not rerun; failure surfaces report error and reruns once deleted; separate Quartz Job; Data Index Job with its DDL credential; stale Job replacement and cleanup |
| `quarkus_config_test.go` | Schema-management env var for `job`/`none` only, never on Quartz; Data Index Flyway toggle |
| `logicflowruntime_controller_test.go` | `setMigrationCondition` matrix (outdated Job, Data Index failure not gating); envtest: Deployment withheld until migration completes, `Failed` phase on migration failure, ephemeral runtimes not gated |
| `clusterlogicplatform_controller_test.go` | envtest: Data Index Deployment withheld until its Job succeeds, then rolled out without Flyway at start |

## Known Gaps

1. **Quartz DDL in the runner is not disabled.** The schema-management env var only covers Hibernate. Whether the runner initializes Quartz tables on its own when a Quartz datastore uses `job`/`none` depends on the runner image.
2. **Populated databases.** Migrating a database that was previously managed by `service` (Data Index ran Flyway with `baselineOnMigrate`) may need a Flyway baseline in the migrator; not tuned yet.
3. **No automated e2e coverage.** The Job → Postgres → runtime rollout path is only covered by unit and envtest tests.
