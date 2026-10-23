package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
)

// helpers

func newTestServer(routes map[routing.WorkflowKey]routing.URL) *Server {
	resolver := routing.NewResolver(routing.NewExecutionEndpointStore(), routing.NewInstanceStore(), nil)
	if routes != nil {
		resolver.UpdateExecutionEndpoints(routes)
	}
	return New(DefaultConfig(), resolver, definitions.NewReader(nil, nil), nil)
}

// mockUpstreamServer creates a test HTTP server that mimics the quarkus-flow runner behavior.
func mockUpstreamServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		// Exec endpoints: /q/flow/exec/{namespace}/{name} or /q/flow/exec/{namespace}/{name}/{version}
		if (path == "/q/flow/exec/demo/hello" || path == "/q/flow/exec/demo/hello/1.0.0") && r.Method == http.MethodPost {
			// Async execution endpoint
			if r.URL.Query().Get("wait") == "false" {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"workflowApplicationId": "test-pod",
					"instanceId":            "test-instance-123",
					"status":                "RUNNING",
				})
				return
			}
			// Sync execution
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": "workflow output",
			})
			return
		}

		// Instance operations: /q/flow/exec/{namespace}/{name}/{version}/{instanceId}/{operation}
		// or /q/flow/exec/{namespace}/{name}/{instanceId}/{operation}
		if (path == "/q/flow/exec/demo/hello/test-instance-123/suspend" ||
			path == "/q/flow/exec/demo/hello/1.0.0/test-instance-123/suspend") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "suspended"})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func newTestServerWithUpstream(routes map[routing.WorkflowKey]routing.URL, upstream *httptest.Server) *Server {
	resolver := routing.NewResolver(routing.NewExecutionEndpointStore(), routing.NewInstanceStore(), nil)
	if routes != nil {
		// Replace URLs with upstream server URL
		updatedRoutes := make(map[routing.WorkflowKey]routing.URL)
		for k := range routes {
			updatedRoutes[k] = routing.URL(upstream.URL)
		}
		resolver.UpdateExecutionEndpoints(updatedRoutes)
	}
	return New(DefaultConfig(), resolver, definitions.NewReader(nil, nil), nil)
}

// Definitions

func TestListDefinitions_Empty(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, "[]", w.Body.String())
}

func TestListDefinitions_ReturnsCatalog(t *testing.T) {
	srv := newTestServer(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL("https://hello.example.com"),
		{Namespace: "prod", Name: "order", Version: "2.0.0"}: routing.URL("https://order.example.com"),
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var defs []routing.WorkflowKey
	err := json.Unmarshal(w.Body.Bytes(), &defs)
	assert.NoError(t, err)
	assert.Len(t, defs, 2)
	// Upstream URL must not be exposed to clients.
	assert.NotContains(t, w.Body.String(), "hello.example.com")
}

func TestListDefinitions_DoesNotExposeURL(t *testing.T) {
	srv := newTestServer(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL("https://hello.example.com"),
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.NotContains(t, w.Body.String(), "hello.example.com")
}

func TestGetDefinition_NotFound(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions/demo/hello/1.0.0", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	// FetchDefinition returns 404 when definition not found in cache
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Instances

func TestListInstances_EndpointNotDiscovered(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/instances", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestGetInstance_EndpointNotDiscovered(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// Runtime — unknown workflow returns 404

func TestExecute_UnknownWorkflow_Returns404(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/missing", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestExecuteVersioned_UnknownWorkflow_Returns404(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/missing/1.0.0", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSuspend_UnknownWorkflow_Returns404(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/missing/instance/abc123/suspend", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Runtime — known workflow proxies to upstream

func TestExecuteAsync_KnownWorkflow_Returns202(t *testing.T) {
	upstream := mockUpstreamServer()
	defer upstream.Close()
	srv := newTestServerWithUpstream(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL(""),
	}, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	assert.NotNil(t, resp["instanceId"])
	assert.NotNil(t, resp["workflowApplicationId"])
}

func TestExecuteAsyncVersioned_KnownWorkflow_Returns202(t *testing.T) {
	upstream := mockUpstreamServer()
	defer upstream.Close()
	srv := newTestServerWithUpstream(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL(""),
	}, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/1.0.0", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestExecuteSync_KnownWorkflow_Returns200(t *testing.T) {
	upstream := mockUpstreamServer()
	defer upstream.Close()
	srv := newTestServerWithUpstream(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL(""),
	}, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/sync", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "workflow output", resp["result"])
}

// Routing disambiguation: literal "instance" must NOT be treated as {version}

func TestSuspend_LiteralInstanceNotVersion(t *testing.T) {
	// If chi incorrectly matched "instance" as {version}, routing.Resolve would
	// call Lookup with version="instance" and find nothing → 404. With correct
	// routing it calls LookupLatest and routes to suspend endpoint.
	upstream := mockUpstreamServer()
	defer upstream.Close()
	srv := newTestServerWithUpstream(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL(""),
	}, upstream)
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/instance/test-instance-123/suspend", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	// 200 = suspend succeeded. 404 = routing bug.
	assert.Equal(t, http.StatusOK, w.Code)
}

// OpenAPI

func TestOpenAPIEndpoint_Returns200(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/openapi.json", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestOpenAPIEndpoint_ValidJSON(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/openapi.json", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var doc map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &doc)
	assert.NoError(t, err, "response must be valid JSON")
	assert.Equal(t, "3.0.3", doc["openapi"], "spec version must be 3.0.3")
}
