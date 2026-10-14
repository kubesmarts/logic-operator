package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/dataindex"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/discovery"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/platform"
)

func TestServerRoutes(t *testing.T) {
	// For unit tests without envtest, create a minimal Discovery with just a route store.
	store := discovery.NewRouteStore()
	disc := discovery.NewDiscovery(nil, store, nil, nil)
	s := New(DefaultConfig(), disc, nil)
	s.SetReady(true)

	tests := []struct {
		path           string
		expectedStatus int
		name           string
	}{
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

func newServerWithMockDI(t *testing.T, diResponse any) *Server {
	t.Helper()
	diSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(diResponse)
	}))
	t.Cleanup(diSrv.Close)

	pStore := platform.NewPlatformStore()
	pStore.SetEndpoint(diSrv.URL)
	c := dataindex.NewClient(pStore)
	store := discovery.NewRouteStore()
	disc := discovery.NewDiscovery(nil, store, nil, nil)
	s := New(DefaultConfig(), disc, c)
	return s
}

func TestListInstancesHandler_EndpointNotDiscovered(t *testing.T) {
	pStore := platform.NewPlatformStore() // no endpoint
	c := dataindex.NewClient(pStore)
	store := discovery.NewRouteStore()
	disc := discovery.NewDiscovery(nil, store, nil, nil)
	s := New(DefaultConfig(), disc, c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances", nil)
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestListInstancesHandler_Returns200(t *testing.T) {
	s := newServerWithMockDI(t, map[string]any{
		"data": map[string]any{
			"getWorkflowInstances": []map[string]any{
				{"id": "i1", "namespace": "ns", "name": "wf", "version": "v1", "status": "RUNNING"},
			},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances?namespace=ns", nil)
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var instances []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&instances); err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0]["id"] != "i1" {
		t.Fatalf("unexpected response: %v", instances)
	}
}

func TestGetInstanceHandler_NotFound(t *testing.T) {
	s := newServerWithMockDI(t, map[string]any{
		"data": map[string]any{"getWorkflowInstance": nil},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/missing", nil)
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestGetInstanceHandler_Returns200(t *testing.T) {
	s := newServerWithMockDI(t, map[string]any{
		"data": map[string]any{
			"getWorkflowInstance": map[string]any{
				"id": "xyz", "namespace": "ns", "name": "wf",
				"version": "v1", "status": "COMPLETED",
			},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/instances/xyz", nil)
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
