package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
)

func TestServerRoutes(t *testing.T) {
	store := discovery.NewRouteStore()
	s := New(DefaultConfig(), store)
	s.SetReady(true)

	tests := []struct {
		path           string
		expectedStatus int
		name           string
	}{
		{"/health", http.StatusOK, "health endpoint"},
		{"/ready", http.StatusOK, "ready endpoint"},
		{"/notfound", http.StatusNotFound, "404 on unknown route"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.path, nil)
			s.router.ServeHTTP(recorder, req)

			if recorder.Code != tt.expectedStatus {
				t.Errorf("expected %d, got %d", tt.expectedStatus, recorder.Code)
			}
		})
	}
}
