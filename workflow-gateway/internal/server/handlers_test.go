package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

func TestDebugRoutesHandler(t *testing.T) {
	store := discovery.NewRouteStore()
	store.Replace(map[discovery.WorkflowKey]string{
		{Namespace: "default", Name: "hello", Version: "1.0"}: "https://hello.example.com",
		{Namespace: "default", Name: "world", Version: "1.0"}: "https://world.example.com",
	})

	cfg := DefaultConfig()
	srv := New(cfg, store)
	req := httptest.NewRequest("GET", "/debug/routes", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "hello")
	assert.Contains(t, w.Body.String(), "world")
	assert.Contains(t, w.Body.String(), "https://hello.example.com")
}

func TestDebugRoutesHandlerEmpty(t *testing.T) {
	store := discovery.NewRouteStore()

	cfg := DefaultConfig()
	srv := New(cfg, store)
	req := httptest.NewRequest("GET", "/debug/routes", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Should return empty JSON array or object
	assert.NotEmpty(t, w.Body.String())
}

func TestHealthHandler(t *testing.T) {
	store := discovery.NewRouteStore()
	cfg := DefaultConfig()
	srv := New(cfg, store)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
