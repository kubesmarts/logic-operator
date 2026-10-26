//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

var (
	// baseURL is the shared runner base URL (http://127.0.0.1:<mapped-port>)
	baseURL string
	// runnerPort is the mapped host port, injected into the resolver so pod
	// routing targets the container.
	runnerPort int
	// runnerContainer holds the container lifecycle
	runnerContainer testcontainers.Container
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start the runner container once for the entire test suite
	var err error
	runnerContainer, baseURL, runnerPort, err = startRunnerContainer(ctx)
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to start runner container: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Print logs at end of test suite
	logs, err := getRunnerLogs(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to read container logs: %v\n", err)
	} else if logs != "" {
		header := "=== Runner Container Logs ==="
		if code != 0 {
			header += " (test failure)"
		}
		_, _ = fmt.Fprintf(os.Stderr, "\n%s\n%s\n=== End Logs ===\n", header, logs)
	}

	// Tear down container
	if runnerContainer != nil {
		_ = runnerContainer.Terminate(ctx)
	}

	os.Exit(code)
}

func TestRunner_HealthCheck(t *testing.T) {
	// This test verifies the container booted and health endpoint is ready.
	// The WaitingFor strategy already gates TestMain until it succeeds,
	// so this is a sanity check that the container is still running.
	t.Log("baseURL:", baseURL)
	t.Log("runnerContainer:", runnerContainer.GetContainerID()[:12])
	// If we reached here, TestMain succeeded, so the container is ready.
}

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

func TestRunner_SyncExecute_HelloWorld(t *testing.T) {
	gw := newHeadlessGateway(t)

	// POST /v1/runtime/demo/hello-world/1.0.0/sync with input.
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/v1/runtime/demo/hello-world/1.0.0/sync", strings.NewReader(`{"name":"Alice"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "sync execute should return 200")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &result), "response must be valid JSON")

	// The runner nests the flow result under workflowOutput, not at top level.
	output, ok := result["workflowOutput"].(map[string]interface{})
	require.True(t, ok, "response must contain workflowOutput object; got: %s", string(body))
	assert.Equal(t, "Hello, Alice!", output["message"], "workflowOutput.message should greet Alice")
}

func TestRunner_AsyncExecute_Sleepy(t *testing.T) {
	gw := newHeadlessGateway(t)

	// POST /v1/runtime/demo/sleepy/1.0.0 (no /sync) → async execution.
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
	require.NoError(t, json.Unmarshal(body, &result), "response must be valid JSON")

	instanceID, ok := result["instanceId"].(string)
	assert.True(t, ok && instanceID != "", "response must contain non-empty instanceId; got: %s", string(body))

	appID, ok := result["workflowApplicationId"].(string)
	assert.True(t, ok && appID != "", "response must contain non-empty workflowApplicationId; got: %s", string(body))
}

func TestRunner_InstanceLifecycle_SuspendResumeCancel(t *testing.T) {
	gw, resolver := newHeadlessGatewayWithResolver(t)

	// Start a sleepy instance; its 10s nap keeps it RUNNING while we operate on it.
	req, err := http.NewRequest(http.MethodPost, gw.URL+"/v1/runtime/demo/sleepy/1.0.0", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "async start must return 202; got: %s", string(body))

	var started map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &started))
	instanceID, ok := started["instanceId"].(string)
	require.True(t, ok && instanceID != "", "must capture instanceId")

	// Clear execution endpoints to isolate lease-based routing: the fallback service path
	// is no longer available, so only a correctly cached lease can route the ops.
	// This validates that instance caching works and the lease lookup → pod resolver → URL construction is correct.
	resolver.UpdateExecutionEndpoints(nil)

	// Each op must resolve via the cached instance lease (lease ID = workflowApplicationId).
	// If caching or lease resolution broke, these would fail to route and return 404/502.
	for _, op := range []string{"suspend", "resume", "cancel"} {
		url := gw.URL + "/v1/runtime/demo/sleepy/1.0.0/instance/" + instanceID + "/" + op
		opReq, err := http.NewRequest(http.MethodPost, url, nil)
		require.NoError(t, err)

		opResp, err := http.DefaultClient.Do(opReq)
		require.NoError(t, err)
		opResp.Body.Close()

		assert.Equal(t, http.StatusOK, opResp.StatusCode, "%s should return 200 (routed to runner via cached lease)", op)
	}
}
