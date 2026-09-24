# Testing a db-migrator image locally

How to check that a given `logic-db-migrator` image, run through the real
`ClusterLogicPlatform` controller against a real Postgres, actually creates the expected schema tables — not
just that the Kubernetes Job reports success.

Uses [`test-db-migration-local.sh`](./test-db-migration-local.sh), which drives the whole
chain: `ClusterLogicPlatform` → Job → your image → Postgres → `LogicFlowRuntime` rollout. See
[`adr/implementation/db-migration-strategy.md`](../adr/implementation/db-migration-strategy.md)
for how that chain fits together.

## Prerequisites

- `kind`, `kubectl`, `go`, `openssl`, and `docker` or `podman` on `PATH`
- The image under test present locally (`docker pull` it first) or pullable by the script

## Quick start

```bash
./hack/test-db-migration-local.sh \
  --create-cluster \
  --image quay.io/kubesmarts/logic-db-migrator:2.0.0-SNAPSHOT \
  --reset-db \
  --keep
```

- `--create-cluster` — only needed the first time / if you don't already have a KIND cluster (`KIND_CLUSTER`, default `logic-operator-dev`). Omit it on later runs to reuse the same cluster.
- `--image` — the image to test. Defaults to `quay.io/kubesmarts/logic-db-migrator:$DB_MIGRATOR_VERSION` from `versions.mk`.
- `--reset-db` — drops and recreates the test database first. Without this, a second run against an already-migrated database will correctly report `appliedCount: 0` (Flyway is idempotent — see the ADR) and you won't be testing table *creation*, just a no-op.
- `--keep` — leaves the test namespace (and Postgres) up after the script exits, so you can inspect it by hand below. Without it, the namespace is deleted as soon as the script finishes.

The script already asserts three things and fails loudly (with Job/Pod logs) if any doesn't hold:
1. The runtime migration in `ClusterLogicPlatform` `status.dbMigrator` reaches `phase: Succeeded`.
2. The `LogicFlowRuntime` Deployment does not exist while the migration is still running.
3. The Deployment then gets `QUARKUS_HIBERNATE_ORM_SCHEMA_MANAGEMENT_STRATEGY=none`.

It does **not** check which tables exist — do that manually, below.

## Verifying the tables

With the namespace still up (`--keep`), find the Postgres pod and list tables:

```bash
NAMESPACE=db-migration-local-test   # default; matches $NAMESPACE if you overrode it

POD=$(kubectl get pod -n "$NAMESPACE" -l app=postgresql -o jsonpath='{.items[0].metadata.name}')

kubectl exec -n "$NAMESPACE" "$POD" -- env PGPASSWORD=migrator-test-only-insecure \
  psql -U migrator -d db_migrator_test -c '\dt'
```

(`POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB` above are the script's defaults —
`$POSTGRES_USER` / `$POSTGRES_PASSWORD` / `$POSTGRES_DB` if you overrode them.)

The runtime Job applies the `runtime` and `quartz` streams (Quartz shares the runtime
datastore unless `spec.runtime.persistence.quartz` is configured), so expect the workflow
runtime tables, the Quartz `qrtz_*` tables, and the Flyway history tables that prove Flyway
ran rather than the container just exiting 0.

Also check the report content, which names each stream's applied version and count:

```bash
kubectl get clusterlogicplatform db-migration-test \
  -o jsonpath='{.status.dbMigrator.migrations[?(@.datastore=="runtime")].streams}'
```

On a fresh database expect `appliedCount` > 0 per stream. `appliedCount: 0` on a
`--reset-db` run means the image didn't actually apply anything — that's a real failure,
not idempotency.

## Cleaning up

If you used `--keep`:

```bash
kubectl delete clusterlogicplatform db-migration-test
kubectl delete namespace db-migration-local-test   # or $NAMESPACE
```

To tear down the whole cluster:

```bash
kind delete cluster --name logic-operator-dev   # or $KIND_CLUSTER
```
