# Workflow Gateway Integration Tests with Quarkus Flow Runner

**Date**: 2026-10-20  
**Status**: Proposed  
**Scope**: Docker-based integration test suite (no Kubernetes required)

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Purpose & Intent](#purpose--intent)
3. [Key Design Decisions](#key-design-decisions)
4. [Architecture & Wiring](#architecture--wiring)
5. [Directory Layout](#directory-layout)
6. [Container Fixture](#container-fixture)
7. [Networking Constraint](#networking-constraint)
8. [Headless Gateway Harness](#headless-gateway-harness)
9. [Workflow Fixtures](#workflow-fixtures)
10. [Test Matrix](#test-matrix)
11. [Out of Scope](#out-of-scope)
12. [Open Implementation Details](#open-implementation-details)

---

## Executive Summary

This specification describes a lightweight integration test suite for the **workflow-gateway** that validates runtime proxying to a real **Quarkus Flow runner** container without requiring Kubernetes, KIND, or the operator control plane.

**Goals:**
- Verify the gateway correctly reverse-proxies sync/async execution requests and instance operations (suspend, resume, cancel) to a live runner.
- Use Docker only; run inside standard Go testing framework.
- Prove lease-based instance routing works end-to-end.
- Reuse existing `hello` and `sleepy` workflow definitions, re-expressed as Quarkus Flow DSL 1.0.0.

---

## Purpose & Intent

The workflow-gateway is responsible for:
1. Receiving client requests (sync/async execute, instance operations).
2. Resolving the target runner pod via lease-based instance routing.
3. Proxying the request to the runner's HTTP endpoint.

This suite isolates and validates step 3 (proxying) against a real **Quarkus Flow runner** container, eliminating Kubernetes dependencies while maintaining confidence in the critical integration path.

---

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Test Framework** | Go `testing` + `testify` | Matches existing gateway style (`internal/server/handlers_test.go`). No external BDD/Gherkin overhead. |
| **Container Management** | `testcontainers-go` | Programmatic, transactional lifecycle; tight integration with Go test harness. |
| **Headless Mode** | Test-only wiring; no changes to `cmd/main.go` | Isolates test concerns; production code unchanged. |
| **Runner Image** | `quay.io/quarkiverse/quarkus-flow-runner:1.2.0-minimal` | Sufficient for all instance operations (suspend/resume/cancel) because instances live in-memory; no Postgres or `-standard` image needed. |
| **Workflow DSL Version** | Serverless Workflow Specification **v1.0.0** (Quarkus Flow format) | Project standard; same as operator config samples. |

---

## Architecture & Wiring

The integration test assembles a **headless gateway** that:
1. Has no Kubernetes API client (`diClient == nil`).
2. Has no controller-runtime manager or CRD cache.
3. Uses a **fake pod resolver** that always returns the host `localhost` for any lease lookup.
4. Proxies requests to the runner container via hardcoded port 8080.

**Data flow:**
```
Test Request
    ↓
Gateway Handler (via httptest.NewServer)
    ↓
Router.Resolver (fake pod resolver: lease → "localhost")
    ↓
Reverse Proxy to http://localhost:8080/{path}
    ↓
Quarkus Flow Runner Container
```

---

## Directory Layout

New test structure under `workflow-gateway/test/integration/`:

```
workflow-gateway/test/integration/
├── testdata/
│   └── workflows/
│       ├── hello.yaml        # document: namespace=demo, name=hello-world, version=1.0.0
│       └── sleepy.yaml       # document: namespace=demo, name=sleepy, version=1.0.0
├── runner_container.go       # testcontainers-go fixture; manages container lifecycle
├── harness.go                # headless gateway assembly (fake resolver, server setup)
└── gateway_runner_test.go    # test cases; carries //go:build integration tag
```

**Build Tag Isolation:**
- All integration tests are gated behind `//go:build integration`.
- Default `make test` / `go test ./...` runs **without** these tests (docker-free).
- New Makefile target: `test-gateway-integration` runs `cd workflow-gateway && go test -tags=integration ./test/integration/...`.

---

## Container Fixture

**File**: `runner_container.go`

The fixture manages the runner container lifecycle using `testcontainers-go`:

- **Image**: `quay.io/quarkiverse/quarkus-flow-runner:1.2.0-minimal`
- **Port Binding**: **Fixed to host port 8080** via `ExposedPorts: ["8080:8080/tcp"]`
- **Volume Mount**: `testdata/workflows` → `/deployments/workflows:ro` (read-only bind mount)
- **Readiness Strategy**: HTTP health check (exact path TBD; likely `/q/health/ready` or fallback to `/`)
- **Lifecycle**: **One container per suite.** The container is started and warmed (readiness-gated) exactly once via `TestMain`, shared across all test cases, and torn down after the entire suite completes. Tests must not assume a fresh runner between cases; the sleepy lifecycle test starts its own instance and drives it to a terminal state (cancel) so it leaves no lingering in-memory instance.

**Rationale for fixed port 8080:** See [Networking Constraint](#networking-constraint) below.

---

## Networking Constraint

**Why port 8080 is pinned (NOT configurable):**

The gateway's `routing.Resolver.ResolveInstance()` (file `workflow-gateway/internal/routing/resolver.go`) constructs the pod URL as:
```
http://{podName}:{port}
```

The production default comes from `api/v1.QuarkusFlowRunnerHTTPPort` (`8080`), shared by the runtime reconciler and gateway. The resolver's `WithRunnerPort` option lets integration tests use testcontainers' dynamically mapped host port while leaving the runner's container port at 8080.

The fake pod resolver (see [Headless Gateway Harness](#headless-gateway-harness)) returns `localhost` and the mapped host port, so the constructed URL reaches the runner without requiring host port 8080 to be free.

This keeps production routing aligned with the runner's pod port while avoiding a fixed host-port requirement in the integration suite.

---

## Headless Gateway Harness

**File**: `harness.go`

The harness assembles the gateway server without Kubernetes dependencies:

```go
// Fake pod resolver: always resolves a lease to "localhost"
type fakePodResolver struct {
    host string
}

func (f fakePodResolver) PodFor(ctx context.Context, namespace, leaseID string) (string, bool, error) {
    return f.host, true, nil
}

// Assembly (pseudo-code)
resolver := routing.NewResolver(
    routing.NewExecutionEndpointStore(),
    routing.NewInstanceStore(),
    fakePodResolver{host: "localhost"},
)

// Register the two test workflows
resolver.UpdateExecutionEndpoints(map[routing.WorkflowKey]routing.URL{
    {Namespace: "demo", Name: "hello-world", Version: "1.0.0"}: "http://localhost:8080",
    {Namespace: "demo", Name: "sleepy",      Version: "1.0.0"}: "http://localhost:8080",
})

// Create server with nil diClient and nil-backed definitions reader
srv := server.New(cfg, resolver, definitions.NewReader(nil, nil), nil)
ts := httptest.NewServer(srv.Handler())
defer ts.Close()
```

**Graceful Degradation (no production changes required):**
- `diClient == nil` → Instance handlers return HTTP 503 "data index not available" (already handled in code).
- `definitions.NewReader(nil, nil)` → `FetchDefinition` returns `ErrDefinitionNotFound` → GET definition handler returns HTTP 404 (already handled in code).

---

## Workflow Fixtures

### `testdata/workflows/hello.yaml`

```yaml
document:
  dsl: '1.0.0'
  namespace: demo
  name: hello-world
  version: '1.0.0'
do:
  - greet:
      set:
        message: '${ "Hello, " + .name + "!" }'
```

### `testdata/workflows/sleepy.yaml`

```yaml
document:
  dsl: '1.0.0'
  namespace: demo
  name: sleepy
  version: '1.0.0'
do:
  - greet:
      set:
        message: 'Starting nap...'
  - nap:
      wait: { seconds: 10 }
  - wakeUp:
      set:
        status: 'Nap complete'
```

**Note**: The exact `wait` task syntax and duration should be cross-checked against the operator sample `config/samples/persistence/logic_v1_logicflowdefinition_sleepy.yaml` during implementation. The nap duration is ~10 seconds to keep the suite fast while allowing time to issue instance operations.

---

## Test Matrix

### Overview

| Test Case | Workflow | Endpoint | Method | Expected Status | Validates |
|-----------|----------|----------|--------|-----------------|-----------|
| Sync Execute | `hello-world` | `/v1/runtime/demo/hello-world/1.0.0/sync` | POST | 200 | Sync execution; response body shape |
| Async Execute | `sleepy` | `/v1/runtime/demo/sleepy/1.0.0` | POST | 202 | Async execution; instance caching |
| Instance Lifecycle | `sleepy` (in-flight) | `.../instance/{id}/{suspend\|resume\|cancel}` | POST | 200 | Lease-based routing; all 3 operations |
| Graceful Degradation | N/A | `/v1/instances`, `/v1/definitions/...` | GET | 503, 404 | k8s-dependent endpoints degrade cleanly |

### Test Descriptions

#### 1. Sync Execute (hello-world)

**Request:**
```
POST /v1/runtime/demo/hello-world/1.0.0/sync
Content-Type: application/json
{ "name": "Alice" }
```

**Expectations:**
- HTTP 200
- Response JSON contains `message` field == `"Hello, Alice!"`
- Execution completes before response.

**Validates**: Sync execution path, request routing, response body marshaling.

---

#### 2. Async Execute (sleepy)

**Request:**
```
POST /v1/runtime/demo/sleepy/1.0.0
Content-Type: application/json
{}
```

**Expectations:**
- HTTP 202 (Accepted)
- Response JSON contains `instanceId` and `workflowApplicationId` fields.
- Gateway's `ModifyResponse` handler caches `instanceId → workflowApplicationId` (lease id) for later instance operations.

**Validates**: Async execution, instance ID generation, response caching.

---

#### 3. Instance Lifecycle (sleepy)

**Setup**: Issue async execute to start a `sleepy` workflow. Capture `instanceId`.

**Sequence** (while the 10-second nap is in progress):

1. **Suspend**
   ```
   POST /v1/runtime/demo/sleepy/1.0.0/instance/{instanceId}/suspend
   ```
   - Expected: HTTP 200
   - Verified: Lease → pod resolution; proxy to runner.

2. **Resume**
   ```
   POST /v1/runtime/demo/sleepy/1.0.0/instance/{instanceId}/resume
   ```
   - Expected: HTTP 200

3. **Cancel**
   ```
   POST /v1/runtime/demo/sleepy/1.0.0/instance/{instanceId}/cancel
   ```
   - Expected: HTTP 200

**Validates**: End-to-end lease-based instance routing (fake resolver → pod name → proxy); all three instance operations proxied correctly.

---

#### 4. Graceful Degradation (no Kubernetes)

**List Instances:**
```
GET /v1/instances
```
- Expected: HTTP 503 (data index unavailable)
- Validates: `diClient == nil` returns 503 as designed.

**Get Definition:**
```
GET /v1/definitions/demo/hello-world/1.0.0
```
- Expected: HTTP 404 (definition not found)
- Validates: nil-backed reader returns `ErrDefinitionNotFound` as designed.

---

## Out of Scope

The following are **explicitly NOT part of this suite**:

- **Production headless mode** in `cmd/main.go`. The wiring is test-only.
- **Postgres / Persistence / `-standard` image**. In-memory instances suffice.
- **KIND / Kubernetes API**. No cluster required.
- **Changes to production routing/server code**. The port-8080 hardcoding is worked around via test harness, not refactored in `resolver.go`.

---

## Open Implementation Details

The following must be confirmed during implementation:

- [ ] **Runner health/readiness endpoint**: Exact HTTP path for the testcontainers wait strategy. Likely `/q/health/ready` (Quarkus default) or fallback to `/`.
- [ ] **Sync execution response body**: Exact JSON shape returned by the runner for sync execution. Confirm the `message` field is present and format.
- [ ] **Async execution response body**: Exact JSON shape for async execution. Confirm `instanceId` and `workflowApplicationId` field names.
- [ ] **Wait task syntax in Quarkus Flow DSL 1.0.0**: Cross-check the `wait:` task syntax in the operator sample `config/samples/persistence/logic_v1_logicflowdefinition_sleepy.yaml` to ensure `sleepy.yaml` fixture matches.
- [ ] **Host port 8080 availability in CI**: Ensure CI environment has port 8080 free before running the suite. Add a pre-flight check if needed.
