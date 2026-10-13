package dataindex_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/dataindex"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/platform"
)

func makeDataIndexServer(response any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
}

func TestListInstances_Returns200WithInstances(t *testing.T) {
	ts := makeDataIndexServer(map[string]any{
		"data": map[string]any{
			"getWorkflowInstances": []map[string]any{
				{"id": "abc", "namespace": "ns", "name": "wf", "version": "v1", "status": "RUNNING"},
			},
		},
	})
	defer ts.Close()

	pStore := platform.NewPlatformStore()
	pStore.SetEndpoint(ts.URL)
	c := dataindex.NewClient(pStore)

	instances, err := c.ListInstances(context.Background(), dataindex.InstanceFilter{Namespace: "ns"})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ID != "abc" || instances[0].Status != "RUNNING" {
		t.Fatalf("unexpected instances: %+v", instances)
	}
}

func TestListInstances_EndpointNotDiscovered(t *testing.T) {
	pStore := platform.NewPlatformStore() // no endpoint
	c := dataindex.NewClient(pStore)
	_, err := c.ListInstances(context.Background(), dataindex.InstanceFilter{})
	if !errors.Is(err, dataindex.ErrEndpointNotDiscovered) {
		t.Fatalf("expected ErrEndpointNotDiscovered, got %v", err)
	}
}

func TestListInstances_GraphQLErrors(t *testing.T) {
	ts := makeDataIndexServer(map[string]any{
		"errors": []map[string]any{{"message": "boom"}},
	})
	defer ts.Close()

	pStore := platform.NewPlatformStore()
	pStore.SetEndpoint(ts.URL)
	c := dataindex.NewClient(pStore)

	_, err := c.ListInstances(context.Background(), dataindex.InstanceFilter{})
	if err == nil {
		t.Fatal("expected error from GraphQL errors array, got nil")
	}
}

func TestGetInstance_ReturnsInstance(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ts := makeDataIndexServer(map[string]any{
		"data": map[string]any{
			"getWorkflowInstance": map[string]any{
				"id": "xyz", "namespace": "ns", "name": "wf",
				"version": "v1", "status": "COMPLETED",
				"startedAt": now.Format(time.RFC3339),
			},
		},
	})
	defer ts.Close()

	pStore := platform.NewPlatformStore()
	pStore.SetEndpoint(ts.URL)
	c := dataindex.NewClient(pStore)

	inst, err := c.GetInstance(context.Background(), "xyz")
	if err != nil {
		t.Fatal(err)
	}
	if inst.ID != "xyz" || inst.Status != "COMPLETED" {
		t.Fatalf("unexpected instance: %+v", inst)
	}
}

func TestGetInstance_NotFound(t *testing.T) {
	ts := makeDataIndexServer(map[string]any{
		"data": map[string]any{"getWorkflowInstance": nil},
	})
	defer ts.Close()

	pStore := platform.NewPlatformStore()
	pStore.SetEndpoint(ts.URL)
	c := dataindex.NewClient(pStore)

	_, err := c.GetInstance(context.Background(), "missing")
	if !errors.Is(err, dataindex.ErrInstanceNotFound) {
		t.Fatalf("expected ErrInstanceNotFound, got %v", err)
	}
}

func TestGetInstance_EndpointNotDiscovered(t *testing.T) {
	pStore := platform.NewPlatformStore()
	c := dataindex.NewClient(pStore)
	_, err := c.GetInstance(context.Background(), "any")
	if !errors.Is(err, dataindex.ErrEndpointNotDiscovered) {
		t.Fatalf("expected ErrEndpointNotDiscovered, got %v", err)
	}
}
