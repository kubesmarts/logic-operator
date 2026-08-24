# Demo Handoff & Docs Kickoff

**Date:** 2026-08-24  
**Status:** Demo postponed — docs work started on `docs/v2-docs`  
**Branch:** `docs/v2-docs`

---

## Demo Readiness (postponed — pick up here when rescheduled)

The demo is fully prepared. When rescheduled, the entire environment can be stood up from scratch in ~5 minutes.

### Local environment (verified 2026-08-24)

| Asset | Status | Note |
|-------|--------|------|
| `main` branch | ✅ Current | PR #27 merged — quarkus-flow-runner 1.0.0 |
| `controller:dev` Docker image | ✅ Cached | `sha256:6e81d84c1cf8` |
| `quarkus-flow-runner:1.0.0-minimal` | ✅ Cached | quay.io image pre-pulled |
| `quarkus-flow-runner:1.0.0-standard` | ✅ Cached | quay.io image pre-pulled |
| `gcr.io/distroless/static:nonroot` | ✅ Cached | required for `make kind-deploy` |
| Architecture slides | ✅ Ready | `docs/architecture-slide.html`, `docs/architecture-slide-2.html` |

### Demo run sequence

```bash
# 1. Create cluster
make kind-create

# 2. Pre-load runner images (avoids quay.io pull delay during demo)
kind load docker-image quay.io/quarkiverse/quarkus-flow-runner:1.0.0-minimal --name logic-operator-dev
kind load docker-image quay.io/quarkiverse/quarkus-flow-runner:1.0.0-standard --name logic-operator-dev

# 3. Deploy operator
make kind-deploy

# 4. Wait for webhook (if kind-demo fails on first try)
kubectl wait --namespace logic-operator-system \
  --for=condition=ready pod -l control-plane=controller-manager --timeout=60s

# 5. Base sample — hello-world
make kind-demo
curl -X POST http://hello.lvh.me/q/flow/exec/default/hello-world/1.0.0 \
  -H "Content-Type: application/json" -d '{"input":{"name":"demo"}}'

# 6. Persistence + monitoring
kubectl apply -k config/samples/monitoring/
kubectl apply -k config/samples/persistence/

# 7. Load + Grafana
for i in $(seq 1 30); do
  curl -s -X POST http://hello.lvh.me/q/flow/exec/default/hello-world/1.0.0 \
    -H "Content-Type: application/json" -d "{\"input\":{\"name\":\"demo-$i\"}}" > /dev/null
done
# Open: http://grafana.lvh.me · http://prometheus.lvh.me

# 8. Slides
open docs/architecture-slide.html
open docs/architecture-slide-2.html
```

### Key endpoints

| Service | URL |
|---------|-----|
| hello-world | http://hello.lvh.me |
| sleepy-workflow (durable) | http://sleepy.lvh.me |
| Runner (health / metrics) | http://runtime.lvh.me |
| Prometheus | http://prometheus.lvh.me |
| Grafana | http://grafana.lvh.me |

### Known gotchas

- `make kind-demo` may fail right after `make kind-deploy` — webhook cert takes ~5s; wait and retry
- `make kind-deploy` may fail with `connection refused` if kubectl context is wrong → `kubectl config use-context kind-logic-operator-dev`

---

## What was shipped (PR #27 — merged 2026-08-21)

**`chore: upgrade quarkus-flow-runner 0.15.1 → 1.0.0`**

| Change | Detail |
|--------|--------|
| Version bump | `QuarkusFlowVersion = "1.0.0"` in `quarkus_constants.go` |
| Removed `WithMetricsEnvVars()` | Micrometer auto-enabled in 1.0.0; workaround for `quarkus-flow#854` deleted |
| Directory mounts | `WithFlowVolumeMounts` reverted from per-key subPath to per-ConfigMap directory. Fixes `quarkus-flow#835` (via `followSymlinks=false`). Kubelet now propagates ConfigMap changes automatically — no pod restart needed for workflow content updates |
| Regression tests | Full non-persistent option set exercised; metrics env var confirmed absent |

Validated: 22/22 e2e specs, live KIND deployment, Grafana dashboards with real workflow data.

---

## Docs Work (started 2026-08-24 on `docs/v2-docs`)

Starting user-facing documentation in `docs/`. See conversation for scope.
