package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

func DefaultManagementConfig() ManagementConfig {
	return ManagementConfig{
		Addr:            ":8081",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}
}

func TestManagementHealthHandler(t *testing.T) {
	store := discovery.NewRouteStore()
	m := NewManagement(DefaultManagementConfig(), store, func() bool { return true })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestManagementReadyHandler_NotReady(t *testing.T) {
	store := discovery.NewRouteStore()
	m := NewManagement(DefaultManagementConfig(), store, func() bool { return false })
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestManagementReadyHandler_Ready(t *testing.T) {
	store := discovery.NewRouteStore()
	m := NewManagement(DefaultManagementConfig(), store, func() bool { return true })
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestManagementDebugRoutesHandler(t *testing.T) {
	store := discovery.NewRouteStore()
	store.Replace(map[discovery.WorkflowKey]string{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: "https://hello.example.com",
	})
	m := NewManagement(DefaultManagementConfig(), store, func() bool { return true })
	req := httptest.NewRequest(http.MethodGet, "/debug/routes", nil)
	w := httptest.NewRecorder()
	m.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "hello")
	assert.Contains(t, w.Body.String(), "https://hello.example.com")
}
