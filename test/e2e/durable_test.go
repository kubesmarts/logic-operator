package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controller "github.com/kubesmarts/logic-operator/internal/controller"
	"github.com/kubesmarts/logic-operator/test/utils"
)

const durableRuntimeName = "hello-runtime"
const durableDefinitionName = "sleepy-workflow-v1-0-0"
const durableServiceName = "sleepy-workflow"
const durableNamespace = "logic-platform"
const durableWorkflowName = "sleepy-workflow"
const durableWorkflowHost = "sleepy.lvh.me"

func durableTests() {
	Context("Durable LogicFlowRuntime with PostgreSQL persistence", Ordered, func() {
		BeforeAll(func() {
			By("applying the persistence sample bundle")
			cmd := exec.Command("kubectl", "apply", "-k", "config/samples/persistence/", "-n", durableNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for PostgreSQL to be ready")
			waitForPG := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod",
					"-l", "app=postgresql",
					"-n", durableNamespace,
					"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			Eventually(waitForPG, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for the sample runtime")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime", durableRuntimeName, "-n", durableNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, 30*time.Second, time.Second).Should(Succeed())
		})

		AfterAll(func() {
			for _, args := range [][]string{
				{"logicflowservice", durableServiceName},
				{"logicflowdefinition", durableDefinitionName},
				{"logicflowruntime", durableRuntimeName},
			} {
				cmd := exec.Command("kubectl", "delete", args[0], args[1],
					"-n", durableNamespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
			cmd := exec.Command("kubectl", "delete", "clusterlogicplatform", "default", "--ignore-not-found")
			_, _ = utils.Run(cmd)
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "clusterlogicplatform", "default")
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).WithTimeout(2 * time.Minute).Should(Succeed())
			cmd = exec.Command("kubectl", "delete", "namespace", durableNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			specReport := CurrentSpecReport()
			if specReport.Failed() {
				By("collecting logs from durable runtime pods on test failure")
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
					"-n", durableNamespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				podNames, err := utils.Run(cmd)
				if err == nil {
					for _, podName := range strings.Fields(podNames) {
						By(fmt.Sprintf("fetching logs from pod %s", podName))
						cmd := exec.Command("kubectl", "logs", podName, "-n", durableNamespace)
						logs, err := utils.Run(cmd)
						if err == nil {
							_, _ = fmt.Fprintf(GinkgoWriter, "Pod %s logs:\n%s\n", podName, logs)
						} else {
							_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get logs from pod %s: %v\n", podName, err)
						}
					}
				}
			}
		})

		It("should create the durable Deployment with 2 ready replicas", func() {
			verifyDeployment := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					durableRuntimeName, "-n", durableNamespace,
					"-o", "jsonpath={.status.availableReplicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("2"))
			}
			Eventually(verifyDeployment, 3*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should create one Lease per replica", func() {
			verifyLeases := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "leases",
					"-n", durableNamespace,
					"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
					"--no-headers")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				lines := strings.Split(strings.TrimSpace(out), "\n")
				g.Expect(lines).To(HaveLen(2))
			}
			Eventually(verifyLeases, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should set each Lease holderIdentity to a running pod name", func() {
			verifyHolders := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
					"-n", durableNamespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				podNames, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				pods := strings.Fields(podNames)
				g.Expect(pods).To(HaveLen(2))

				cmd = exec.Command("kubectl", "get", "leases",
					"-n", durableNamespace,
					"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
					"-o", "jsonpath={.items[*].spec.holderIdentity}")
				holders, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Fields(holders)).To(HaveLen(len(pods)))
				for _, h := range strings.Fields(holders) {
					g.Expect(pods).To(ContainElement(h))
				}
			}
			Eventually(verifyHolders, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should recover an in-flight workflow after pod crash", func() {
			// The operator overrides flow.document.namespace with the CR's Kubernetes namespace.
			// URL format: /q/flow/exec/{document.namespace}/{document.name}/{document.version}
			svcURL := fmt.Sprintf("http://%s/q/flow/exec/%s/%s/1.0.0", durableWorkflowHost, durableNamespace, durableWorkflowName)
			healthURL := "http://" + durableWorkflowHost + "/q/health/ready"

			By("verifying runtime pods are ready to accept requests")
			verifyHealthy := func(g Gomega) {
				c := exec.Command("curl", "-sf", healthURL)
				out, err := c.CombinedOutput()
				g.Expect(err).NotTo(HaveOccurred(), "health check failed: %s", string(out))
			}
			Eventually(verifyHealthy, 5*time.Minute, 5*time.Second).Should(Succeed())

			By("starting a sleepy workflow instance without retries")
			c := exec.Command("curl", "-sf", "-X", "POST",
				"-H", "Content-Type: application/json",
				"-d", `{"name":"e2e"}`,
				svcURL)
			out, err := c.CombinedOutput()
			Expect(err).NotTo(HaveOccurred())
			var invocation struct {
				WorkflowApplicationID string `json:"workflowApplicationId"`
				InstanceID            string `json:"instanceId"`
				Status                string `json:"status"`
			}
			Expect(json.Unmarshal(out, &invocation)).To(Succeed(), "workflow response: %s", string(out))
			Expect(invocation.WorkflowApplicationID).NotTo(BeEmpty(), "expected workflowApplicationId in response")
			Expect(invocation.InstanceID).NotTo(BeEmpty(), "expected instanceId in response")

			By("finding the pod that holds the workflow's application lease")
			cmd := exec.Command("kubectl", "get", "lease", invocation.WorkflowApplicationID,
				"-n", durableNamespace, "-o", "jsonpath={.spec.holderIdentity}")
			podName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			podName = strings.TrimSpace(podName)
			Expect(podName).NotTo(BeEmpty(), "lease %q has no holder", invocation.WorkflowApplicationID)
			_, _ = fmt.Fprintf(GinkgoWriter,
				"Durable workflow invocation: applicationId=%s instanceId=%s status=%s ownerPod=%s\n",
				invocation.WorkflowApplicationID, invocation.InstanceID, invocation.Status, podName)

			By("allowing asynchronous persistence a moment to commit before killing the owner")
			time.Sleep(2 * time.Second)

			By("killing the pod that owns the workflow lease")
			_, err = utils.Run(exec.Command("kubectl", "delete", "pod",
				podName, "-n", durableNamespace, "--grace-period=0", "--force"))
			Expect(err).NotTo(HaveOccurred())

			By("waiting for 2 ready replicas again after crash")
			verifyTwoReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					durableRuntimeName, "-n", durableNamespace,
					"-o", "jsonpath={.status.availableReplicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("2"))
			}
			Eventually(verifyTwoReady, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for the same lease to move to the replacement pod")
			replacementPodName := ""
			verifyLeaseRecovery := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "lease", invocation.WorkflowApplicationID,
					"-n", durableNamespace, "-o", "jsonpath={.spec.holderIdentity}")
				holder, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				holder = strings.TrimSpace(holder)
				g.Expect(holder).NotTo(BeEmpty(), "lease %q has no holder", invocation.WorkflowApplicationID)
				g.Expect(holder).NotTo(Equal(podName), "expected lease to move off the deleted pod")

				cmd = exec.Command("kubectl", "get", "pod", holder,
					"-n", durableNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				ready, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ready).To(Equal("True"), "lease holder pod %q is not Ready", holder)
				replacementPodName = holder
			}
			Eventually(verifyLeaseRecovery, 3*time.Minute, 2*time.Second).Should(Succeed())
			_, _ = fmt.Fprintf(GinkgoWriter,
				"Durable workflow lease recovered: applicationId=%s replacementPod=%s\n",
				invocation.WorkflowApplicationID, replacementPodName)

			By("checking the replacement pod has the recovered workflow instance in memory")
			runnerURL, stopPortForward, err := startRunnerPortForward(replacementPodName)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(stopPortForward)
			var lastInstancesOutput string
			var lastInstancesError string
			var recoveredStatus string
			verifyRestored := func() bool {
				c := exec.Command("curl", "-fsS", runnerURL+"/q/flow/instances")
				instancesOutput, err := c.CombinedOutput()
				lastInstancesOutput = string(instancesOutput)
				lastInstancesError = ""
				if err != nil {
					lastInstancesError = err.Error()
					return false
				}
				var activeInstances struct {
					ApplicationID string `json:"applicationId"`
					Instances     []struct {
						InstanceID string `json:"instanceId"`
						Status     string `json:"status"`
					} `json:"instances"`
				}
				if err := json.Unmarshal(instancesOutput, &activeInstances); err != nil {
					lastInstancesError = fmt.Sprintf("decode response: %v", err)
					return false
				}
				if activeInstances.ApplicationID != invocation.WorkflowApplicationID {
					lastInstancesError = fmt.Sprintf("unexpected applicationId %q", activeInstances.ApplicationID)
					return false
				}
				for _, instance := range activeInstances.Instances {
					if instance.InstanceID == invocation.InstanceID {
						recoveredStatus = instance.Status
						return true
					}
				}
				lastInstancesError = "instance is absent from the active-instance response"
				return false
			}
			Eventually(verifyRestored, 3*time.Minute, 2*time.Second).Should(BeTrue(),
				"expected active instance on replacement pod; applicationId=%s instanceId=%s ownerPod=%s replacementPod=%s lastResponseError=%s lastResponse:\n%s",
				invocation.WorkflowApplicationID, invocation.InstanceID, podName, replacementPodName,
				lastInstancesError, lastInstancesOutput)
			_, _ = fmt.Fprintf(GinkgoWriter,
				"Durable workflow instance recovered: applicationId=%s instanceId=%s status=%s replacementPod=%s\n",
				invocation.WorkflowApplicationID, invocation.InstanceID, recoveredStatus, replacementPodName)
		})

		It("should preserve both Leases after scaling down to 1 replica", func() {
			By("recording the two leases before scale-down")
			cmd := exec.Command("kubectl", "get", "leases",
				"-n", durableNamespace,
				"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
				"-o", "jsonpath={.items[*].metadata.name}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			leaseNames := strings.Fields(out)
			Expect(leaseNames).To(HaveLen(2))

			By("scaling the LogicFlowRuntime down to one replica")
			cmd = exec.Command("kubectl", "patch", "logicflowruntime", durableRuntimeName,
				"-n", durableNamespace, "--type=merge", "-p", `{"spec":{"replicas":1}}`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the Deployment to have one available replica")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", durableRuntimeName,
					"-n", durableNamespace,
					"-o", "jsonpath={.spec.replicas} {.status.availableReplicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Fields(out)).To(Equal([]string{"1", "1"}))
			}, time.Minute, 5*time.Second).Should(Succeed())

			By("verifying that the original leases remain after scale-down")
			verifyLeasePersist := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "leases",
					"-n", durableNamespace,
					"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
					"-o", "jsonpath={.items[*].metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Fields(out)).To(ConsistOf(leaseNames[0], leaseNames[1]))
			}
			Eventually(verifyLeasePersist, time.Minute, 5*time.Second).Should(Succeed())
		})
	})
}

func startRunnerPortForward(podName string) (string, func(), error) {
	return startRunnerPortForwardInNamespace(durableNamespace, podName)
}

func startRunnerPortForwardInNamespace(namespace, podName string) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return "", nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
		"--address=127.0.0.1", "-n", namespace,
		"pod/"+podName, fmt.Sprintf("%d:8080", localPort))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, err
	}
	stop := func() {
		cancel()
		_ = cmd.Wait()
	}
	return fmt.Sprintf("http://127.0.0.1:%d", localPort), stop, nil
}
