package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	s := New(DefaultConfig())
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	s.healthHandler(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", recorder.Header().Get("Content-Type"))
	}
}

func TestReadyHandlerNotReady(t *testing.T) {
	s := New(DefaultConfig())
	s.SetReady(false)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)

	s.readyHandler(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", recorder.Code)
	}
}

func TestReadyHandlerReady(t *testing.T) {
	s := New(DefaultConfig())
	s.SetReady(true)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)

	s.readyHandler(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", recorder.Code)
	}
}
