package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
)

type discoveryPodResolver struct {
	leaseID string
	podName string
}

func (p discoveryPodResolver) PodFor(_ context.Context, _ string, leaseID string) (string, bool, error) {
	if leaseID != p.leaseID {
		return "", false, nil
	}
	return p.podName, true, nil
}

func TestInstanceOperation_DiscoversOwnerAndRoutesToPod(t *testing.T) {
	var discoveryCalls, serviceOperationCalls, podCalls int
	runtimeService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/q/flow/demo/hello/1.0.0/discovery/instance-1" {
			discoveryCalls++
			assert.Empty(t, r.Header.Get("Authorization"), "discovery uses gateway service-account auth, not caller credentials")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"applicationId": "lease-1"})
			return
		}
		serviceOperationCalls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer runtimeService.Close()

	pod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		podCalls++
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/q/flow/exec/demo/hello/1.0.0/instance-1/suspend", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer pod.Close()

	address := strings.TrimPrefix(pod.URL, "http://")
	host, portText, err := net.SplitHostPort(address)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	resolver := routing.NewResolver(
		routing.NewExecutionEndpointStore(),
		routing.NewInstanceStore(),
		discoveryPodResolver{leaseID: "lease-1", podName: host},
		routing.WithRunnerPort(port),
	)
	key := routing.WorkflowKey{Namespace: "demo", Name: "hello", Version: "1.0.0"}
	resolver.UpdateRuntimeEndpoints(map[routing.WorkflowKey]routing.URL{key: routing.URL(runtimeService.URL)})
	resolver.UpdateExecutionEndpoints(map[routing.WorkflowKey]routing.URL{key: routing.URL(runtimeService.URL)})
	srv := New(DefaultConfig(), resolver, definitions.NewReader(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/1.0.0/instance/instance-1/suspend", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, discoveryCalls)
	assert.Equal(t, 0, serviceOperationCalls)
	assert.Equal(t, 1, podCalls)
	assert.True(t, resolver.HasCachedInstance("instance-1"))
}

func TestInstanceOperation_Discovery404FallsBackToRuntimeService(t *testing.T) {
	var discoveryCalls, operationCalls int
	runtimeService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			discoveryCalls++
			w.WriteHeader(http.StatusNotFound)
			return
		}
		operationCalls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer runtimeService.Close()

	resolver := routing.NewResolver(routing.NewExecutionEndpointStore(), routing.NewInstanceStore(), nil)
	key := routing.WorkflowKey{Namespace: "demo", Name: "hello", Version: "1.0.0"}
	resolver.UpdateRuntimeEndpoints(map[routing.WorkflowKey]routing.URL{key: routing.URL(runtimeService.URL)})
	resolver.UpdateExecutionEndpoints(map[routing.WorkflowKey]routing.URL{key: routing.URL(runtimeService.URL)})
	srv := New(DefaultConfig(), resolver, definitions.NewReader(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/1.0.0/instance/missing/suspend", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, 1, discoveryCalls)
	assert.Equal(t, 1, operationCalls)
}

func TestInstanceOperation_UnversionedDiscoveryTriesWorkflowVersions(t *testing.T) {
	var queried []string
	runtimeService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		queried = append(queried, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/1.0.0/discovery/instance-1") {
			_ = json.NewEncoder(w).Encode(map[string]string{"applicationId": "lease-1"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer runtimeService.Close()

	resolver := routing.NewResolver(routing.NewExecutionEndpointStore(), routing.NewInstanceStore(), nil)
	resolver.UpdateRuntimeEndpoints(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL(runtimeService.URL),
		{Namespace: "demo", Name: "hello", Version: "2.0.0"}: routing.URL(runtimeService.URL),
	})
	resolver.UpdateExecutionEndpoints(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello", Version: "1.0.0"}: routing.URL(runtimeService.URL),
		{Namespace: "demo", Name: "hello", Version: "2.0.0"}: routing.URL(runtimeService.URL),
	})
	srv := New(DefaultConfig(), resolver, definitions.NewReader(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/runtime/demo/hello/instance/instance-1/suspend", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusBadGateway, w.Code)
	assert.Equal(t, []string{
		"/q/flow/demo/hello/2.0.0/discovery/instance-1",
		"/q/flow/demo/hello/1.0.0/discovery/instance-1",
	}, queried)
}

func TestDiscoveryURL_AppendsToBasePathAndEscapesSegments(t *testing.T) {
	got, err := discoveryURL("http://runner.example/base", "demo space", "hello", "1.0.0", "id/1")
	require.NoError(t, err)
	assert.Equal(t, "http://runner.example/base/q/flow/demo%20space/hello/1.0.0/discovery/id%2F1", got)
	_, err = discoveryURL(":bad", "ns", "name", "1", "id")
	assert.Error(t, err)
}
