//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/kubesmarts/logic-operator/internal/controller"
)

// startRunnerContainer starts the Quarkus Flow runner container with workflows
// mounted from the local testdata/workflows directory. It returns the container,
// the base URL, and the mapped host port.
//
// A testcontainers-assigned random host port is used, and its value is returned
// so the caller can inject it via routing.WithRunnerPort — resolver.go builds pod
// URLs as http://<host>:<port>, so pod routing must target the mapped port.
//
// Readiness is gated by polling the definitions endpoint over the host loopback
// (the exact path the gateway dials), not by testcontainers' built-in wait. Two
// reasons: the runner reports health-ready before it finishes loading workflow
// definitions (a non-empty array proves they are registered), and under colima
// the VM->host port forward takes a few seconds to establish after publish while
// testcontainers' own check uses a different path (the VM address) that is ready
// sooner. Gating on 127.0.0.1 closes that race; otherwise the gateway's dial
// beats the forward and returns 502.
func startRunnerContainer(ctx context.Context) (testcontainers.Container, string, int, error) {
	image := fmt.Sprintf("%s/%s:%s-%s",
		logicv1.FlowRunnerRegistry,
		logicv1.FlowRunnerImage,
		controller.QuarkusFlowVersion,
		controller.ImageVariantMinimal)
	portTCP := fmt.Sprintf("%d/tcp", controller.QuarkusPort)

	// Mount local testdata workflows into the container
	workflowSrc, err := workflowTestdataPath()
	if err != nil {
		return nil, "", 0, err
	}

	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{portTCP},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(workflowSrc, controller.WorkflowMountPath),
		),
		// Log wait is read over the docker socket (reliable) and confirms the JVM
		// booted; real readiness is verified by the host-side poll below.
		WaitingFor: wait.ForLog("Installed features").WithStartupTimeout(120 * time.Second),
		SkipReaper: true, // We manage cleanup in TestMain
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to start runner container: %w", err)
	}

	// Verify workflows are in the container
	exitCode, reader, err := c.Exec(ctx, []string{"ls", "-1", controller.WorkflowMountPath})
	outputBytes, _ := io.ReadAll(reader)
	output := string(outputBytes)

	if err != nil || exitCode != 0 || !strings.Contains(output, ".yaml") {
		c.Terminate(ctx)
		return nil, "", 0, fmt.Errorf("workflow files verification failed (exit %d): %s", exitCode, output)
	}

	mappedPort, err := c.MappedPort(ctx, portTCP)
	if err != nil {
		c.Terminate(ctx)
		return nil, "", 0, fmt.Errorf("failed to get mapped port: %w", err)
	}

	port := int(mappedPort.Num())
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	if err := waitForDefinitionsOverHost(ctx, baseURL, 120*time.Second); err != nil {
		c.Terminate(ctx)
		return nil, "", 0, err
	}

	return c, baseURL, port, nil
}

// waitForDefinitionsOverHost polls the definitions endpoint on the host loopback
// until it returns a non-empty JSON array or the timeout elapses.
func waitForDefinitionsOverHost(ctx context.Context, baseURL string, timeout time.Duration) error {
	url := baseURL + "/q/flow/definitions"
	deadline := time.Now().Add(timeout)
	for {
		if ok := definitionsReady(ctx, url); ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("runner definitions not reachable at %s within %s", url, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func definitionsReady(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var defs []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&defs); err != nil {
		return false
	}
	return len(defs) > 0
}

// workflowTestdataPath returns the absolute path to the testdata/workflows directory.
func workflowTestdataPath() (string, error) {
	// This file is in workflow-gateway/test/integration/
	// testdata/workflows is one level up from test/
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Navigate from current dir to testdata/workflows
	path := filepath.Join(wd, "testdata", "workflows")
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", fmt.Errorf("testdata/workflows not found at %s: %w", path, err)
	}
	return path, nil
}
