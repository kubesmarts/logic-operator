# Gateway↔Runner Integration Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a lightweight, Docker-based integration test suite validating that the workflow-gateway correctly reverse-proxies sync/async execution and instance operations to a live Quarkus Flow runner container without Kubernetes.

**Architecture:** Gateway receives client requests, fake pod resolver always returns `localhost`, hardcoded port 8080 routes to the container. One container per test suite, shared across all cases. Test-only wiring via `httptest.Server`; no changes to production code.

**Tech Stack:** Go 1.26.0, testify, testcontainers-go, quay.io/quarkiverse/quarkus-flow-runner:1.2.0-minimal

**Spec:** adr/2026-10-20-gateway-runner-integration-tests.md

## Global Constraints

- Module path: `github.com/kubesmarts/logic-operator/workflow-gateway`
- All integration files must carry `//go:build integration` tag
- No license/copyright headers on new files (automated script adds them)
- **Commit policy (project rule):** Do NOT run `git commit` without the user's exact phrase `APPROVED: commit`
- Host port 8080 must be free to run the test suite

## Review Focus

- Container boot and health-check timeout handling (exact endpoint path for readiness)
- Port 8080 already in use at test time (no pre-flight validation in tests themselves)
- Sync response body shape from runner differs from assumptions (e.g., field names)
- Async response body shape from runner differs (e.g., `instanceId` vs `id`)
- `wait:` task DSL syntax in sleepy.yaml doesn't match runner's Quarkus Flow DSL 1.0.0
- Instance operations routed via fake resolver to container but container doesn't honor suspend/resume/cancel (unlikely but verify)

---

## Task 1: Scaffolding & Dependency

**Files:**
- Create: `workflow-gateway/test/integration/`
- Modify: `workflow-gateway/go.mod`
- Modify: `Makefile` (root)

**Interfaces:**
- Produces: Package `github.com/kubesmarts/logic-operator/workflow-gateway/test/integration` with `//go:build integration` tag

- [ ] **Step 1: Create test integration package**
```bash
mkdir -p /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway/test/integration
```

- [ ] **Step 2: Create empty `init_test.go` to establish package**
```go
//go:build integration

package integration

// Placeholder to ensure package compiles under integration tag.
```

- [ ] **Step 3: Add testcontainers-go dependency to go.mod**
```bash
cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway
go get github.com/testcontainers/testcontainers-go@latest
```

- [ ] **Step 4: Verify go.mod updated**
Run: `grep testcontainers /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway/go.mod`
Expected: Entry like `github.com/testcontainers/testcontainers-go v...`

- [ ] **Step 5: Add Makefile target at root**
Edit `/Users/ricferna/dev/github/kubesmarts/logic-operator/Makefile` to add:
```makefile
.PHONY: test-gateway-integration
test-gateway-integration:
	cd workflow-gateway && go test -tags=integration ./test/integration/... -v
```

- [ ] **Step 6: Verify target exists**
Run: `grep -A2 "test-gateway-integration:" /Users/ricferna/dev/github/kubesmarts/logic-operator/Makefile`
Expected: Target and command visible

---

## Task 2: Workflow Fixtures

**Files:**
- Create: `workflow-gateway/test/integration/testdata/workflows/hello.yaml`
- Create: `workflow-gateway/test/integration/testdata/workflows/sleepy.yaml`
- Create: `workflow-gateway/test/integration/fixtures_test.go` (test to verify fixtures)

**Interfaces:**
- Produces: Two valid Quarkus Flow DSL 1.0.0 YAML files; test that reads and parses them

- [ ] **Step 1: Create testdata directory**
```bash
mkdir -p /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway/test/integration/testdata/workflows
```

- [ ] **Step 2: Create hello.yaml fixture**
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

- [ ] **Step 3: Create sleepy.yaml fixture**
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

- [ ] **Step 4: Create fixtures_test.go to verify both files parse**
```go
//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWorkflowFixtures_HelloYAML(t *testing.T) {
	helloPath := filepath.Join("testdata", "workflows", "hello.yaml")
	data, err := os.ReadFile(helloPath)
	require.NoError(t, err, "hello.yaml must be readable")

	var doc map[string]interface{}
	err = yaml.Unmarshal(data, &doc)
	require.NoError(t, err, "hello.yaml must parse as YAML")

	docNode := doc["document"].(map[string]interface{})
	assert.Equal(t, "demo", docNode["namespace"], "namespace must be demo")
	assert.Equal(t, "hello-world", docNode["name"], "name must be hello-world")
	assert.Equal(t, "1.0.0", docNode["version"], "version must be 1.0.0")
}

func TestWorkflowFixtures_SleepyYAML(t *testing.T) {
	sleepyPath := filepath.Join("testdata", "workflows", "sleepy.yaml")
	data, err := os.ReadFile(sleepyPath)
	require.NoError(t, err, "sleepy.yaml must be readable")

	var doc map[string]interface{}
	err = yaml.Unmarshal(data, &doc)
	require.NoError(t, err, "sleepy.yaml must parse as YAML")

	docNode := doc["document"].(map[string]interface{})
	assert.Equal(t, "demo", docNode["namespace"], "namespace must be demo")
	assert.Equal(t, "sleepy", docNode["name"], "name must be sleepy")
	assert.Equal(t, "1.0.0", docNode["version"], "version must be 1.0.0")
}
```

- [ ] **Step 5: Run fixture tests**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -run TestWorkflowFixtures -v`
Expected: Both tests PASS

---

## Task 3: Container Fixture & TestMain

**Files:**
- Create: `workflow-gateway/test/integration/runner_container.go`
- Modify: `workflow-gateway/test/integration/gateway_runner_test.go` (add TestMain)

**Interfaces:**
- Consumes: testcontainers-go types, `context.Context`
- Produces: `baseURL string` (global), `container.Container`, ready signal

- [ ] **Step 1: Create runner_container.go**

Uses `HostConfigModifier` for BOTH the read-only bind mount and the fixed host-port binding. Do NOT use the deprecated `testcontainers.Mounts`/`BindMount` API, and do NOT put the host port in `ExposedPorts` (testcontainers-go does not reliably honor a host port there — the `nat.PortMap` below is what pins it). The `HostConfigModifier` signature in current testcontainers-go is `func(*container.HostConfig)` with no error return.

```go
//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startRunnerContainer starts the Quarkus Flow runner container with the workflow
// fixtures mounted read-only and the container's 8080 pinned to host 8080.
// It returns the container and the base URL (http://localhost:8080).
func startRunnerContainer(ctx context.Context) (testcontainers.Container, string, error) {
	workflowsPath, err := filepath.Abs(filepath.Join("testdata", "workflows"))
	if err != nil {
		return nil, "", fmt.Errorf("cannot resolve workflows path: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "quay.io/quarkiverse/quarkus-flow-runner:1.2.0-minimal",
		ExposedPorts: []string{"8080/tcp"},
		WaitingFor: wait.ForHTTP("/q/health/ready").
			WithPort("8080/tcp").
			WithStartupTimeout(120 * time.Second),
		HostConfigModifier: func(hc *container.HostConfig) {
			// Read-only bind mount of the workflow fixtures.
			hc.Binds = append(hc.Binds, workflowsPath+":/deployments/workflows:ro")
			// Pin the container's 8080 to host 8080 (required: pod URLs hardcode :8080).
			hc.PortBindings = nat.PortMap{
				"8080/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "8080"}},
			}
		},
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to start runner container: %w", err)
	}
	return c, "http://localhost:8080", nil
}
```

Note: `github.com/docker/docker` and `github.com/docker/go-connections` are transitive deps of testcontainers-go; after `go get` in Task 1 they resolve automatically, but run `go mod tidy` if the imports are not found.

- [ ] **Step 2: Create gateway_runner_test.go with TestMain**
```go
//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

var (
	// baseURL is the shared gateway base URL (http://localhost:8080)
	baseURL string
	// runnerContainer holds the container lifecycle
	runnerContainer testcontainers.Container
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start the runner container once for the entire test suite
	var err error
	runnerContainer, baseURL, err = startRunnerContainer(ctx)
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to start runner container: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Tear down container
	if runnerContainer != nil {
		_ = runnerContainer.Terminate(ctx)
	}

	os.Exit(code)
}
```

- [ ] **Step 3: Create simple health check test**
```go
func TestRunner_HealthCheck(t *testing.T) {
	// This test verifies the container booted and health endpoint is ready.
	// The WaitingFor strategy already gates TestMain until it succeeds,
	// so this is a sanity check that the container is still running.
	t.Log("baseURL:", baseURL)
	t.Log("runnerContainer:", runnerContainer.GetContainerID()[:12])
	// If we reached here, TestMain succeeded, so the container is ready.
}
```

- [ ] **Step 4: Run TestMain and health check**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -run TestRunner_HealthCheck -v`
Expected: PASS; container logs show readiness detected

- [ ] **Step 5: Check container logs if health fails**
If WaitingFor timeout occurs, inspect the container logs (printed to stderr during startup). Adjust health path if needed (e.g., `/` instead of `/q/health/ready`). Document the final path in the runner_container.go comment.

---

## Task 4: Headless Harness

**Files:**
- Create: `workflow-gateway/test/integration/harness.go`

**Interfaces:**
- Consumes: `server.New`, `server.DefaultConfig`, `routing.Resolver`, `routing.NewResolver`, `routing.NewExecutionEndpointStore`, `routing.NewInstanceStore`, `routing.WorkflowKey`, `routing.URL`, `definitions.NewReader`, `httptest.NewServer`, `PodResolver` interface
- Produces: `fakePodResolver` type, `newHeadlessGateway(*testing.T) *httptest.Server` function

- [ ] **Step 1: Create harness.go**
```go
//go:build integration

package integration

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
)

// fakePodResolver implements routing.PodResolver.
// It always returns the provided host (localhost) for any lease lookup.
type fakePodResolver struct {
	host string
}

// PodFor returns the fake host, always found, with no error.
func (f fakePodResolver) PodFor(ctx context.Context, namespace, leaseID string) (string, bool, error) {
	return f.host, true, nil
}

// newHeadlessGateway creates a gateway server wired with a fake pod resolver
// and seeded execution endpoints for both test workflows pointing at baseURL.
// It returns an httptest.Server ready to accept requests.
func newHeadlessGateway(t *testing.T) *httptest.Server {
	t.Helper()

	// Create resolver with fake pod resolver
	resolver := routing.NewResolver(
		routing.NewExecutionEndpointStore(),
		routing.NewInstanceStore(),
		fakePodResolver{host: "localhost"},
	)

	// Register both test workflows, pointing at the runner container
	resolver.UpdateExecutionEndpoints(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello-world", Version: "1.0.0"}: routing.URL(baseURL),
		{Namespace: "demo", Name: "sleepy", Version: "1.0.0"}:      routing.URL(baseURL),
	})

	// Create server with nil clients (no K8s, no data-index)
	srv := server.New(
		server.DefaultConfig(),
		resolver,
		definitions.NewReader(nil, nil),
		nil, // diClient
	)

	// Wrap in httptest.Server
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })

	return ts
}
```

- [ ] **Step 2: Add degradation tests to gateway_runner_test.go**
```go
import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_Degradation_ListInstances(t *testing.T) {
	// Headless gateway with diClient == nil should return 503
	gw := newHeadlessGateway(t)

	req, err := http.NewRequest(http.MethodGet, gw.URL+"/v1/instances", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "should return 503 with no data-index")
	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]string
	_ = json.Unmarshal(body, &errResp)
	assert.Contains(t, errResp["error"], "data index", "error should mention data index")
}

func TestGateway_Degradation_GetDefinition(t *testing.T) {
	// Headless gateway with nil reader should return 404
	gw := newHeadlessGateway(t)

	req, err := http.NewRequest(http.MethodGet, gw.URL+"/v1/definitions/demo/hello-world/1.0.0", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "should return 404 with nil reader")
	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]string
	_ = json.Unmarshal(body, &errResp)
	assert.Contains(t, errResp["error"], "not found", "error should mention not found")
}
```

- [ ] **Step 3: Run degradation tests**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -run TestGateway_Degradation -v`
Expected: Both tests PASS; 503 and 404 respectively

---

## Task 5: Sync Execute Test

**Files:**
- Modify: `workflow-gateway/test/integration/gateway_runner_test.go`

**Interfaces:**
- Consumes: `newHeadlessGateway`, `baseURL`, `http.NewRequest`, `http.DefaultClient`, `routing.WorkflowKey`
- Produces: Test `TestRunner_SyncExecute_HelloWorld` validating sync execution

- [ ] **Step 1: Write sync execute test**
```go
func TestRunner_SyncExecute_HelloWorld(t *testing.T) {
	// Connect to headless gateway
	gw := newHeadlessGateway(t)

	// POST /v1/runtime/demo/hello-world/1.0.0/sync with input
	reqBody := `{"name":"Alice"}`
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/v1/runtime/demo/hello-world/1.0.0/sync", strings.NewReader(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "sync execute should return 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "response must be valid JSON")

	// Verify response contains message field
	message, ok := result["message"].(string)
	assert.True(t, ok, "response must contain message field (string)")
	assert.Equal(t, "Hello, Alice!", message, "message should be Hello, Alice!")
}
```

- [ ] **Step 2: Add import for strings**
Ensure imports include: `"strings"`

- [ ] **Step 3: Run sync execute test**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -run TestRunner_SyncExecute -v`
Expected: PASS if runner returns `{"message":"Hello, Alice!"}`; FAIL if response shape differs (open item #2)

---

## Task 6: Async Execute Test

**Files:**
- Modify: `workflow-gateway/test/integration/gateway_runner_test.go`

**Interfaces:**
- Consumes: `newHeadlessGateway`, `routing.Resolver.CacheInstance` (called by gateway internally)
- Produces: Test `TestRunner_AsyncExecute_Sleepy` validating async execution and instance caching

- [ ] **Step 1: Write async execute test**
```go
func TestRunner_AsyncExecute_Sleepy(t *testing.T) {
	// Connect to headless gateway
	gw := newHeadlessGateway(t)

	// POST /v1/runtime/demo/sleepy/1.0.0 (async, no version in URL)
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/v1/runtime/demo/sleepy/1.0.0", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode, "async execute should return 202")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "response must be valid JSON")

	// Verify response contains instanceId and workflowApplicationId
	instanceID, ok := result["instanceId"].(string)
	assert.True(t, ok, "response must contain instanceId field (string)")
	assert.NotEmpty(t, instanceID, "instanceId must not be empty")

	workflowAppID, ok := result["workflowApplicationId"].(string)
	assert.True(t, ok, "response must contain workflowApplicationId field (string)")
	assert.NotEmpty(t, workflowAppID, "workflowApplicationId must not be empty")
}
```

- [ ] **Step 2: Run async execute test**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -run TestRunner_AsyncExecute -v`
Expected: PASS if runner returns `{"instanceId":"...","workflowApplicationId":"...","status":"RUNNING"}`; FAIL if field names differ (open item #3)

---

## Task 7: Instance Lifecycle Test

**Files:**
- Modify: `workflow-gateway/test/integration/gateway_runner_test.go`

**Interfaces:**
- Consumes: `newHeadlessGateway`, async result parsing
- Produces: Test `TestRunner_InstanceLifecycle_SuspendResumeCancel` validating all three operations

- [ ] **Step 1: Write instance lifecycle test**
```go
func TestRunner_InstanceLifecycle_SuspendResumeCancel(t *testing.T) {
	// Connect to headless gateway
	gw := newHeadlessGateway(t)

	// Step 1: Async execute to start sleepy workflow
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/v1/runtime/demo/sleepy/1.0.0", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode, "async execute must succeed")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var asyncResp map[string]interface{}
	err = json.Unmarshal(body, &asyncResp)
	require.NoError(t, err)

	instanceID, ok := asyncResp["instanceId"].(string)
	require.True(t, ok, "instanceId must be present and string")

	// Step 2: Suspend instance
	suspendURL := gw.URL + "/v1/runtime/demo/sleepy/1.0.0/instance/" + instanceID + "/suspend"
	req, err = http.NewRequest(http.MethodPost, suspendURL, nil)
	require.NoError(t, err)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "suspend should return 200")

	// Step 3: Resume instance
	resumeURL := gw.URL + "/v1/runtime/demo/sleepy/1.0.0/instance/" + instanceID + "/resume"
	req, err = http.NewRequest(http.MethodPost, resumeURL, nil)
	require.NoError(t, err)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "resume should return 200")

	// Step 4: Cancel instance (cleanup so no lingering instance)
	cancelURL := gw.URL + "/v1/runtime/demo/sleepy/1.0.0/instance/" + instanceID + "/cancel"
	req, err = http.NewRequest(http.MethodPost, cancelURL, nil)
	require.NoError(t, err)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "cancel should return 200")
}
```

- [ ] **Step 2: Run instance lifecycle test**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -run TestRunner_InstanceLifecycle -v`
Expected: PASS; all three operations return 200

- [ ] **Step 3: Run all integration tests together**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test -tags=integration ./test/integration/... -v`
Expected: All 7+ tests PASS

- [ ] **Step 4: Verify default `make test` excludes integration tests**
Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator/workflow-gateway && go test ./... -v 2>&1 | grep -c integration || echo "0"`
Expected: 0 (no integration tests run without `-tags=integration`)

- [ ] **Step 5: Verify new Makefile target runs integration tests**
Run: `make -C /Users/ricferna/dev/github/kubesmarts/logic-operator test-gateway-integration 2>&1 | head -20`
Expected: Integration tests start running; container is fetched/started

---

## Commit & Stage (after all tasks complete)

Once all tests pass:

- [ ] **Step 1: Stage all new files**
```bash
cd /Users/ricferna/dev/github/kubesmarts/logic-operator
git add workflow-gateway/test/integration/
git add workflow-gateway/go.mod
git add Makefile
```

- [ ] **Step 2: Show git status**
```bash
git status
```
Expected: Files staged, no uncommitted changes

- [ ] **Step 3: Show diff of staged changes**
```bash
git diff --staged | head -100
```

- [ ] **Step 4: Wait for user approval**
**MUST receive exact phrase: `APPROVED: commit`**

- [ ] **Step 5: Commit (only after approval)**
```bash
git commit -m "$(cat <<'EOF'
feat(#38): Add gateway↔runner integration tests with testcontainers

- New test/integration/ package with //go:build integration tag
- Container fixture: start Quarkus Flow runner 1.2.0-minimal once per suite
- Headless gateway harness: fake pod resolver → localhost:8080
- Workflow fixtures: hello-world and sleepy YAML files (DSL 1.0.0)
- Test matrix: sync execute, async execute, instance lifecycle (suspend/resume/cancel)
- Graceful degradation tests: 503 for data-index, 404 for definitions
- Makefile target: test-gateway-integration runs suite with Docker
- Default make test unchanged (no -tags=integration flag)

Validates lease-based instance routing end-to-end without Kubernetes.

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 6: Verify commit created**
```bash
git log --oneline -3
```
Expected: New commit at HEAD

---

## Summary

This plan implements 7 sequential tasks to deliver the gateway↔runner integration test suite:

1. **Scaffolding & Dependency:** Create test package, add testcontainers-go, add Makefile target
2. **Workflow Fixtures:** Write hello.yaml and sleepy.yaml, validate with YAML parser tests
3. **Container Fixture:** Start runner container once in TestMain, gate readiness with /q/health/ready
4. **Headless Harness:** Fake pod resolver, httptest.Server wiring, degradation tests
5. **Sync Execute:** POST /sync → 200, response `message` field verified
6. **Async Execute:** POST /async → 202, response `instanceId` and `workflowApplicationId` captured
7. **Instance Lifecycle:** Async start → suspend → resume → cancel, all 200, no lingering instances

All tests use plain Go testing + testify; carry `//go:build integration`; are excluded from default `make test`; run via `make test-gateway-integration` with `testcontainers-go` orchestration. Container is shared per suite (TestMain), pinned to port 8080 (no configurable routing port in resolver). No production code changes.

**Open implementation details to confirm at test run:**
- Runner health endpoint path (likely `/q/health/ready`, fallback `/`)
- Sync response body shape (message field assumed)
- Async response body shape (instanceId, workflowApplicationId field names)
- sleepy.yaml wait syntax (cross-check against operator sample)
- Port 8080 availability in CI environment
