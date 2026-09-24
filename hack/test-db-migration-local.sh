#!/usr/bin/env bash
# Exercises the db-migrator "job" strategy end-to-end against a real KIND cluster:
#   operator (go run, out-of-cluster) -> ClusterLogicPlatform -> real Job -> real Postgres,
#   then a LogicFlowRuntime to confirm it rolls out only after the migration and with
#   QUARKUS_HIBERNATE_ORM_SCHEMA_MANAGEMENT_STRATEGY=none
#   (see adr/implementation/db-migration-strategy.md).
#
# Requires: kind, kubectl, go, docker (or podman), openssl. A KIND cluster must already
# exist (see --create-cluster to have this script create one).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

KIND_CLUSTER="${KIND_CLUSTER:-logic-operator-dev}"
NAMESPACE="${NAMESPACE:-db-migration-local-test}"
MIGRATION_IMAGE="${MIGRATION_IMAGE:-quay.io/kubesmarts/logic-db-migrator:$(make -s print-versions | sed -n 's/^db_migrator_version=//p')}"
POSTGRES_DB="${POSTGRES_DB:-db_migrator_test}"
POSTGRES_USER="${POSTGRES_USER:-migrator}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-migrator-test-only-insecure}"
TIMEOUT_SECS="${TIMEOUT_SECS:-120}"
PLATFORM_NAME="db-migration-test"
RUNTIME_NAME="db-migration-test-rt"

CREATE_CLUSTER=0
KEEP=0
RESET_DB=0

usage() {
	cat <<EOF
Usage: $(basename "$0") [options]

Options:
  --create-cluster   Create the KIND cluster (\$KIND_CLUSTER, default "$KIND_CLUSTER") if missing.
  --image IMAGE      db-migrator image to test (default: \$MIGRATION_IMAGE, "$MIGRATION_IMAGE").
  --reset-db         Drop and recreate the test database before running (forces a fresh migration).
  --keep             Don't delete the test namespace on exit (operator process is always stopped).
  -h, --help         Show this help.

Env overrides: KIND_CLUSTER, NAMESPACE, MIGRATION_IMAGE, POSTGRES_DB, POSTGRES_USER,
POSTGRES_PASSWORD, TIMEOUT_SECS.
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--create-cluster) CREATE_CLUSTER=1 ;;
	--image) MIGRATION_IMAGE="$2"; shift ;;
	--reset-db) RESET_DB=1 ;;
	--keep) KEEP=1 ;;
	-h | --help) usage; exit 0 ;;
	*) echo "Unknown option: $1" >&2; usage; exit 1 ;;
	esac
	shift
done

log() { echo "[test-db-migration] $*" >&2; }
fail() { echo "[test-db-migration] FAIL: $*" >&2; exit 1; }

for bin in kind kubectl go openssl; do
	command -v "$bin" >/dev/null 2>&1 || fail "'$bin' is required but not on PATH"
done
CONTAINER_TOOL="$(command -v docker || command -v podman || true)"
[ -n "$CONTAINER_TOOL" ] || fail "docker or podman is required but neither is on PATH"

OPERATOR_PID=""
WORKDIR="$(mktemp -d)"
# Tracks whether $NAMESPACE existed before this script touched it (e.g. "default").
# Cleanup NEVER deletes a namespace it didn't create itself — only the specific named
# resources this script created within it.
NS_PRE_EXISTED=0

cleanup() {
	if [ -n "$OPERATOR_PID" ] && kill -0 "$OPERATOR_PID" 2>/dev/null; then
		log "stopping operator (pid $OPERATOR_PID)"
		kill "$OPERATOR_PID" 2>/dev/null || true
		wait "$OPERATOR_PID" 2>/dev/null || true
	fi
	if [ "$KEEP" -eq 1 ]; then
		log "--keep set: leaving namespace $NAMESPACE and workdir $WORKDIR in place (operator log: $OPERATOR_LOG)"
	elif [ "$NS_PRE_EXISTED" -eq 1 ]; then
		log "namespace $NAMESPACE pre-existed this run — deleting only the resources this script created in it, not the namespace itself"
		kubectl delete -n "$NAMESPACE" --ignore-not-found --wait=false \
			logicflowruntime/"$RUNTIME_NAME" \
			deployment/postgresql service/postgresql secret/pg-creds \
			>/dev/null 2>&1 || true
		kubectl delete clusterlogicplatform "$PLATFORM_NAME" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		rm -rf "$WORKDIR"
	else
		log "deleting ClusterLogicPlatform $PLATFORM_NAME and namespace $NAMESPACE"
		kubectl delete clusterlogicplatform "$PLATFORM_NAME" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
		rm -rf "$WORKDIR"
	fi
}
trap cleanup EXIT

# --- cluster ---------------------------------------------------------------
if ! kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
	if [ "$CREATE_CLUSTER" -eq 1 ]; then
		log "creating KIND cluster '$KIND_CLUSTER'"
		kind create cluster --name "$KIND_CLUSTER"
	else
		fail "KIND cluster '$KIND_CLUSTER' not found. Re-run with --create-cluster, or set KIND_CLUSTER to an existing one."
	fi
fi
kubectl config use-context "kind-$KIND_CLUSTER" >/dev/null

# --- CRDs --------------------------------------------------------------
log "installing CRDs"
bin/kustomize build config/crd | kubectl apply --server-side -f - >/dev/null

# --- namespace + throwaway Postgres -----------------------------------------
kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 && NS_PRE_EXISTED=1
log "applying namespace + Postgres in '$NAMESPACE'$([ "$NS_PRE_EXISTED" -eq 1 ] && echo ' (pre-existing namespace — will not be deleted on cleanup)')"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl apply -n "$NAMESPACE" -f - >/dev/null <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: pg-creds
type: Opaque
stringData:
  POSTGRESQL_USER: "$POSTGRES_USER"
  POSTGRESQL_PASSWORD: "$POSTGRES_PASSWORD"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgresql
spec:
  replicas: 1
  selector:
    matchLabels: { app: postgresql }
  template:
    metadata:
      labels: { app: postgresql }
    spec:
      containers:
        - name: postgresql
          image: postgres:16-alpine
          env:
            - name: POSTGRES_USER
              valueFrom: { secretKeyRef: { name: pg-creds, key: POSTGRESQL_USER } }
            - name: POSTGRES_PASSWORD
              valueFrom: { secretKeyRef: { name: pg-creds, key: POSTGRESQL_PASSWORD } }
            - name: POSTGRES_DB
              value: "$POSTGRES_DB"
          ports:
            - containerPort: 5432
          readinessProbe:
            exec: { command: ["pg_isready", "-U", "$POSTGRES_USER"] }
            initialDelaySeconds: 3
            periodSeconds: 2
---
apiVersion: v1
kind: Service
metadata:
  name: postgresql
spec:
  selector: { app: postgresql }
  ports:
    - port: 5432
      targetPort: 5432
EOF

log "waiting for Postgres to be ready"
kubectl wait -n "$NAMESPACE" --for=condition=available deployment/postgresql --timeout="${TIMEOUT_SECS}s"

if [ "$RESET_DB" -eq 1 ]; then
	log "resetting database '$POSTGRES_DB'"
	POD="$(kubectl get pod -n "$NAMESPACE" -l app=postgresql -o jsonpath='{.items[0].metadata.name}')"
	kubectl exec -n "$NAMESPACE" "$POD" -- env PGPASSWORD="$POSTGRES_PASSWORD" \
		psql -U "$POSTGRES_USER" -d postgres -c "DROP DATABASE IF EXISTS $POSTGRES_DB;" >/dev/null
	kubectl exec -n "$NAMESPACE" "$POD" -- env PGPASSWORD="$POSTGRES_PASSWORD" \
		psql -U "$POSTGRES_USER" -d postgres -c "CREATE DATABASE $POSTGRES_DB OWNER $POSTGRES_USER;" >/dev/null
fi

# --- migration image ---------------------------------------------------
if ! "$CONTAINER_TOOL" image inspect "$MIGRATION_IMAGE" >/dev/null 2>&1; then
	log "image '$MIGRATION_IMAGE' not present locally, attempting pull"
	"$CONTAINER_TOOL" pull "$MIGRATION_IMAGE" || fail "image '$MIGRATION_IMAGE' not found locally and pull failed — build/push it first, or pass --image"
fi
log "loading '$MIGRATION_IMAGE' into KIND cluster '$KIND_CLUSTER'"
kind load docker-image "$MIGRATION_IMAGE" --name "$KIND_CLUSTER"

# --- operator ------------------------------------------------------------
CERT_DIR="$WORKDIR/webhook-certs"
mkdir -p "$CERT_DIR"
openssl req -x509 -newkey rsa:2048 -nodes \
	-keyout "$CERT_DIR/tls.key" -out "$CERT_DIR/tls.crt" \
	-days 1 -subj "/CN=localhost" >/dev/null 2>&1

OPERATOR_LOG="$WORKDIR/operator.log"
log "starting operator (go run ./cmd/main.go), log: $OPERATOR_LOG"
go run ./cmd/main.go \
	--health-probe-bind-address=:0 \
	--metrics-bind-address=:0 \
	--webhook-cert-path="$CERT_DIR" \
	>"$OPERATOR_LOG" 2>&1 &
OPERATOR_PID=$!

for _ in $(seq 1 30); do
	grep -q "Starting Controller.*clusterlogicplatform" "$OPERATOR_LOG" 2>/dev/null && break
	kill -0 "$OPERATOR_PID" 2>/dev/null || fail "operator exited early — see $OPERATOR_LOG"
	sleep 1
done
grep -q "Starting Controller.*clusterlogicplatform" "$OPERATOR_LOG" 2>/dev/null || fail "operator did not report the clusterlogicplatform controller starting — see $OPERATOR_LOG"
log "operator is up"

# --- ClusterLogicPlatform: drives the runtime migration Job ----------------
log "applying ClusterLogicPlatform '$PLATFORM_NAME' (dbMigrationStrategy=job, image=$MIGRATION_IMAGE)"
kubectl apply -f - >/dev/null <<EOF
apiVersion: logic.kubesmarts.org/v1
kind: ClusterLogicPlatform
metadata:
  name: $PLATFORM_NAME
spec:
  targetNamespace: $NAMESPACE
  runtime:
    persistence:
      runtime:
        dbMigrationStrategy: job
        postgresql:
          secretRef:
            name: pg-creds
          serviceRef:
            name: postgresql
            databaseName: $POSTGRES_DB
  dbMigrator:
    application:
      image: $MIGRATION_IMAGE
EOF

# --- LogicFlowRuntime: created up front to observe the rollout gate --------
log "applying LogicFlowRuntime '$RUNTIME_NAME' (PlatformDB)"
kubectl apply -n "$NAMESPACE" -f - >/dev/null <<EOF
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: $RUNTIME_NAME
spec:
  persistenceMode: PlatformDB
  platformRef:
    name: $PLATFORM_NAME
EOF

log "waiting for the runtime migration to finish"
deadline=$((SECONDS + TIMEOUT_SECS))
phase=""
while [ "$SECONDS" -lt "$deadline" ]; do
	phase="$(kubectl get clusterlogicplatform "$PLATFORM_NAME" -o jsonpath='{.status.dbMigrator.migrations[?(@.datastore=="runtime")].phase}' 2>/dev/null || true)"
	[ "$phase" = "Succeeded" ] || [ "$phase" = "Failed" ] && break
	if [ -z "$phase" ] || [ "$phase" = "Pending" ] || [ "$phase" = "Running" ]; then
		kubectl get deployment -n "$NAMESPACE" "$RUNTIME_NAME" >/dev/null 2>&1 &&
			fail "Runtime Deployment exists before the migration finished (phase=${phase:-<none>})"
	fi
	sleep 2
done

if [ "$phase" != "Succeeded" ]; then
	echo "--- ClusterLogicPlatform status ---" >&2
	kubectl get clusterlogicplatform "$PLATFORM_NAME" -o jsonpath='{.status}' >&2 || true
	echo >&2
	echo "--- Job pod logs ---" >&2
	JOB="$(kubectl get clusterlogicplatform "$PLATFORM_NAME" -o jsonpath='{.status.dbMigrator.migrations[?(@.datastore=="runtime")].jobRef}' 2>/dev/null || true)"
	[ -n "$JOB" ] && kubectl logs -n "$NAMESPACE" "job/$JOB" --all-containers >&2 || true
	echo "--- operator log (tail) ---" >&2
	tail -n 60 "$OPERATOR_LOG" >&2 || true
	fail "migration did not reach Succeeded (phase=${phase:-<none>}) within ${TIMEOUT_SECS}s"
fi
REPORT="$(kubectl get clusterlogicplatform "$PLATFORM_NAME" -o jsonpath='{.status.dbMigrator.migrations[?(@.datastore=="runtime")].streams}')"
log "migration Succeeded — streams: $REPORT"

log "waiting for the Runtime's Deployment to appear"
deadline=$((SECONDS + TIMEOUT_SECS))
strategy_env=""
while [ "$SECONDS" -lt "$deadline" ]; do
	strategy_env="$(kubectl get deployment -n "$NAMESPACE" "$RUNTIME_NAME" \
		-o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="QUARKUS_HIBERNATE_ORM_SCHEMA_MANAGEMENT_STRATEGY")].value}' 2>/dev/null || true)"
	[ -n "$strategy_env" ] && break
	sleep 2
done

if [ "$strategy_env" != "none" ]; then
	echo "--- Runtime Deployment env ---" >&2
	kubectl get deployment -n "$NAMESPACE" "$RUNTIME_NAME" \
		-o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' >&2 || true
	fail "QUARKUS_HIBERNATE_ORM_SCHEMA_MANAGEMENT_STRATEGY was not set to 'none' on the Runtime container"
fi

log "PASS — migration succeeded, then the Runtime rolled out with Hibernate schema management disabled"
