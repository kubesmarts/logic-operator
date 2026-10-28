package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubesmarts/logic-operator/api/version"
	"github.com/kubesmarts/logic-operator/test/utils"
)

func workflowGatewayTests() {
	const testNamespace = "e2e-gateway-test"
	const gatewayHost = "gateway.lvh.me"

	var _ = Context("WorkflowGateway", Ordered, func() {
		BeforeAll(func() {
			By("creating test namespace")
			cmd := exec.Command("kubectl", "create", "namespace", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")

			By("creating LogicPlatform with gateway enabled")
			lpYAML := fmt.Sprintf(`
apiVersion: logic.kubesmarts.org/v1
kind: LogicPlatform
metadata:
  name: gateway-test-platform
  namespace: %s
spec:
  dataIndex:
    enabled: false
  workflowGateway:
    application:
      image: kubesmarts.org/workflow-gateway:v%s
    ingress:
      enabled: true
      host: %s
`, testNamespace, version.GetWorkflowGatewayVersion(), gatewayHost)

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewReader([]byte(lpYAML))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicPlatform")

			By("waiting for gateway Deployment to be ready")
			verifyGatewayReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					"gateway-test-platform-workflow-gateway",
					"-n", testNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type==\"Available\")].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Gateway deployment not yet ready")
			}
			Eventually(verifyGatewayReady, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying gateway Service exposes port 80")
			cmd = exec.Command("kubectl", "get", "service",
				"gateway-test-platform-workflow-gateway",
				"-n", testNamespace,
				"-o", "jsonpath={.spec.ports[0].port}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("80"), "Gateway service should expose port 80")

			By("creating LogicFlowRuntime")
			runtimeYAML := fmt.Sprintf(`
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: gateway-test-runtime
  namespace: %s
spec:
  replicas: 1
`, testNamespace)

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewReader([]byte(runtimeYAML))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowRuntime")

			By("creating sleepy workflow definition")
			defYAML := fmt.Sprintf(`
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: e2e-gateway-sleepy-v1-0-0
  namespace: %s
spec:
  runtimeRef:
    name: gateway-test-runtime
  flow:
    document:
      dsl: "1.0.0"
      namespace: e2e
      name: sleepy-workflow
      version: "1.0.0"
    do:
      - greet:
          set:
            message: '${ "Hello from gateway! Sleeping 30s..." }'
      - nap:
          wait:
            seconds: 30
      - wake:
          set:
            message: "Workflow complete"
`, testNamespace)

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = bytes.NewReader([]byte(defYAML))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowDefinition")
		})

		AfterAll(func() {
			By("cleaning up test namespace")
			cmd := exec.Command("kubectl", "delete", "namespace", testNamespace, "--ignore-not-found")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete test namespace")
		})

		It("should route workflow execution through gateway and support suspend/resume lifecycle", func() {
			const gatewayURL = "http://" + gatewayHost

			By("waiting for the workflow to appear in the gateway catalog")
			verifyWorkflowRoute := func(g Gomega) {
				resp, err := http.Get(gatewayURL + "/v1/definitions")
				if err != nil {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
					return
				}

				var routes []struct {
					Namespace string `json:"namespace"`
					Name      string `json:"name"`
					Version   string `json:"version"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				found := false
				for _, route := range routes {
					if route.Namespace == testNamespace && route.Name == "sleepy-workflow" && route.Version == "1.0.0" {
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(), "workflow route not present in gateway catalog: %#v", routes)
			}
			Eventually(verifyWorkflowRoute, 30*time.Second, time.Second).Should(Succeed())

			By("waiting for the runtime to apply the workflow and become ready")
			verifyRuntimeReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime", "gateway-test-runtime",
					"-n", testNamespace,
					"-o", "jsonpath={.status.phase}")
				phase, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(phase).To(Equal("Ready"), "Runtime is not ready after applying the workflow")

				cmd = exec.Command("kubectl", "get", "endpoints", "gateway-test-runtime",
					"-n", testNamespace,
					"-o", "jsonpath={.subsets[*].addresses[*].ip}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Runtime Service has no ready endpoints")
			}
			Eventually(verifyRuntimeReady, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("opening a port-forward to the runtime metrics endpoint")
			cmd := exec.Command("kubectl", "get", "pods",
				"-l", "app.kubernetes.io/name=gateway-test-runtime",
				"-n", testNamespace,
				"-o", "jsonpath={.items[0].metadata.name}")
			runtimePod, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			runtimePod = strings.TrimSpace(runtimePod)
			Expect(runtimePod).NotTo(BeEmpty(), "Runtime pod not found")
			runnerURL, stopPortForward, err := startRunnerPortForwardInNamespace(testNamespace, runtimePod)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(stopPortForward)

			readCompletedCount := func() (float64, error) {
				resp, err := http.Get(runnerURL + "/q/metrics")
				if err != nil {
					return 0, err
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					return 0, fmt.Errorf("metrics endpoint returned %s", resp.Status)
				}
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return 0, err
				}
				count, found, err := workflowCompletedMetric(string(body), "sleepy-workflow", "1.0.0")
				if err != nil {
					return 0, err
				}
				if !found {
					return 0, nil
				}
				return count, nil
			}
			var completedCountBefore float64
			Eventually(func(g Gomega) {
				count, err := readCompletedCount()
				g.Expect(err).NotTo(HaveOccurred())
				completedCountBefore = count
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

			By("posting async workflow execution through gateway ingress")
			execReq := map[string]interface{}{
				"workflowDefinitionId": "sleepy-workflow",
				"workflowVersion":      "1.0.0",
				"name":                 "TestUser",
			}
			execBody, err := json.Marshal(execReq)
			Expect(err).NotTo(HaveOccurred())

			execURL := fmt.Sprintf("%s/v1/runtime/%s/sleepy-workflow/1.0.0", gatewayURL, testNamespace)
			resp, err := http.Post(execURL, "application/json", bytes.NewReader(execBody))
			Expect(err).NotTo(HaveOccurred(), "Failed to post workflow execution")
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted), "Execution should be accepted")

			var execResp map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&execResp)
			Expect(err).NotTo(HaveOccurred())
			instanceID, ok := execResp["instanceId"].(string)
			Expect(ok).To(BeTrue(), "Response should include instanceId")
			Expect(instanceID).NotTo(BeEmpty(), "instanceId should not be empty")

			By("suspending the workflow instance")
			suspendURL := fmt.Sprintf("%s/v1/runtime/%s/sleepy-workflow/instance/%s/suspend", gatewayURL, testNamespace, instanceID)
			suspendReq, err := http.NewRequest("POST", suspendURL, nil)
			Expect(err).NotTo(HaveOccurred())
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err = client.Do(suspendReq)
			Expect(err).NotTo(HaveOccurred(), "Failed to suspend instance")
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).To(Equal(http.StatusOK), "Suspend should succeed")

			By("resuming the workflow instance")
			resumeURL := fmt.Sprintf("%s/v1/runtime/%s/sleepy-workflow/instance/%s/resume", gatewayURL, testNamespace, instanceID)
			resumeReq, err := http.NewRequest("POST", resumeURL, nil)
			Expect(err).NotTo(HaveOccurred())
			resp, err = client.Do(resumeReq)
			Expect(err).NotTo(HaveOccurred(), "Failed to resume instance")
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).To(Equal(http.StatusOK), "Resume should succeed")

			By("waiting for workflow to complete")
			verifyCompletion := func(g Gomega) {
				completedCount, err := readCompletedCount()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(completedCount).To(BeNumerically(">", completedCountBefore),
					"workflow completion metric has not increased")
			}
			Eventually(verifyCompletion, 2*time.Minute, 10*time.Second).Should(Succeed())
		})
	})
}

func workflowCompletedMetric(metrics, workflow, workflowVersion string) (float64, bool, error) {
	for _, line := range strings.Split(metrics, "\n") {
		if !strings.HasPrefix(line, "quarkus_flow_workflow_completed_total{") ||
			!strings.Contains(line, `workflow="`+workflow+`"`) ||
			!strings.Contains(line, `workflowVersion="`+workflowVersion+`"`) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false, fmt.Errorf("malformed workflow completion metric %q", line)
		}
		count, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		return count, true, err
	}
	return 0, false, nil
}
