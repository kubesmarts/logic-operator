package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

// helpers

func newTestServer(routes map[discovery.WorkflowKey]string) *Server {
	store := discovery.NewRouteStore()
	if routes != nil {
		store.Replace(routes)
	}
	return New(DefaultConfig(), store)
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
	srv := newTestServer(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
		{Namespace: "prod", Name: "order", Version: "2.0.0"}: "https://order.example.com",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var defs []discovery.WorkflowKey
	err := json.Unmarshal(w.Body.Bytes(), &defs)
	assert.NoError(t, err)
	assert.Len(t, defs, 2)
	// Upstream URL must not be exposed to clients.
	assert.NotContains(t, w.Body.String(), "hello.example.com")
}

func TestListDefinitions_DoesNotExposeURL(t *testing.T) {
	srv := newTestServer(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.NotContains(t, w.Body.String(), "hello.example.com")
}

func TestGetDefinition_NotImplemented(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions/demo/hello/1.0.0", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// Instances

func TestListInstances_NotImplemented(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/instances", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestGetInstance_NotImplemented(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
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

// Runtime — known workflow returns 501 (found, not yet implemented)

func TestExecute_KnownWorkflow_Returns501(t *testing.T) {
	srv := newTestServer(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestExecuteVersioned_KnownWorkflow_Returns501(t *testing.T) {
	srv := newTestServer(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/1.0.0", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// Routing disambiguation: literal "instance" must NOT be treated as {version}

func TestSuspend_LiteralInstanceNotVersion(t *testing.T) {
	// If chi incorrectly matched "instance" as {version}, routing.Resolve would
	// call Lookup with version="instance" and find nothing → 404. With correct
	// routing it calls LookupLatest → 501 (found, not implemented).
	srv := newTestServer(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/instance/abc123/suspend", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	// 501 = found workflow, proxy not implemented yet. 404 = routing bug.
	assert.Equal(t, http.StatusNotImplemented, w.Code)
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
