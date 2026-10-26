//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/definitions"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/routing"
	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/server"
)

// fakePodResolver implements routing.PodResolver, always resolving any lease to
// the configured host. The host includes both IP and port so that instance ops
// route correctly to the container's mapped port.
type fakePodResolver struct {
	hostPort string
}

func (f fakePodResolver) PodFor(ctx context.Context, namespace, leaseID string) (string, bool, error) {
	return f.hostPort, true, nil
}

// newHeadlessGatewayWithResolver assembles the gateway and returns both the test server
// and the resolver, allowing tests to manipulate routing state. The resolver is
// seeded with execution endpoints for both test workflows pointing at the runner
// (baseURL), uses a fake pod resolver for instance ops, and has nil-backed definitions
// reader (→ 404) and nil data-index client (→ 503).
func newHeadlessGatewayWithResolver(t *testing.T) (*httptest.Server, *routing.Resolver) {
	t.Helper()

	resolver := routing.NewResolver(
		routing.NewExecutionEndpointStore(),
		routing.NewInstanceStore(),
		fakePodResolver{hostPort: "127.0.0.1"},
		routing.WithRunnerPort(runnerPort),
	)
	resolver.UpdateExecutionEndpoints(map[routing.WorkflowKey]routing.URL{
		{Namespace: "demo", Name: "hello-world", Version: "1.0.0"}: routing.URL(baseURL),
		{Namespace: "demo", Name: "sleepy", Version: "1.0.0"}:      routing.URL(baseURL),
	})

	srv := server.New(
		server.DefaultConfig(),
		resolver,
		definitions.NewReader(nil, nil),
		nil, // diClient
	)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })
	return ts, resolver
}

// newHeadlessGateway is a convenience wrapper that returns only the test server.
func newHeadlessGateway(t *testing.T) *httptest.Server {
	t.Helper()
	ts, _ := newHeadlessGatewayWithResolver(t)
	return ts
}

// getRunnerLogs reads and returns the runner container's stdout/stderr logs.
func getRunnerLogs(ctx context.Context) (string, error) {
	if runnerContainer == nil {
		return "", fmt.Errorf("runner container not initialized")
	}

	logs, err := runnerContainer.Logs(ctx)
	if err != nil {
		return "", err
	}
	defer logs.Close()

	logBytes, err := io.ReadAll(logs)
	if err != nil {
		return "", err
	}

	return string(logBytes), nil
}
