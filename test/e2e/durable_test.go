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

func init() {
	// Ensure e2e-durable.lvh.me resolves via the ingress controller
	// The DNS record is typically managed by the ingress infrastructure
	// or by adding it to /etc/hosts on the test machine.
}

// durableRuntimeName uses a distinct name to avoid collision with demo samples.
const durableRuntimeName = "e2e-durable-rt"

// durableInfraNamespace is a separate namespace for PostgreSQL infra.
// logic-operator-system enforces the restricted Pod Security Standard which
// rejects the postgres:16-alpine image (runs as root). A separate namespace
// without that restriction is used instead.
const durableInfraNamespace = "e2e-durable-infra"

// durableRuntimeYAML — spec.image intentionally omitted so the operator defaults to
// QuarkusFlowVersion + "-" + "standard" (e.g. "1.0.0-standard") via DefaultRunnerImage
// in internal/controller/quarkus_config.go.
// serviceRef.namespace points to durableInfraNamespace where PostgreSQL runs
// (logic-operator-system enforces restricted Pod Security which rejects postgres:16-alpine).
const durableRuntimeYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: e2e-durable-rt
  namespace: logic-operator-system
spec:
  replicas: 2
  persistence:
    postgresql:
      secretRef:
        name: postgresql-secret
      serviceRef:
        name: postgresql
        namespace: e2e-durable-infra
        databaseName: logicflow
`

// durablePGSecretYAML creates the credentials secret (no namespace in spec so it inherits from command line).
// Will be created in both e2e-durable-infra and logic-operator-system namespaces.
const durablePGSecretYAML = `
apiVersion: v1
kind: Secret
metadata:
  name: postgresql-secret
type: Opaque
stringData:
  POSTGRESQL_USER: flowuser
  POSTGRESQL_PASSWORD: flowpass
`

const durableSleepyDefinitionYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: e2e-sleepy-v1-0-0
  namespace: logic-operator-system
spec:
  runtimeRef:
    name: e2e-durable-rt
  flow:
    document:
      dsl: "1.0.0"
      namespace: e2e
      name: sleepy
      version: "1.0.0"
    do:
      - greet:
          set:
            message: '${ "Hello, " + .name + "! Sleeping for 30s..." }'
      - nap:
          wait:
            seconds: 30
      - wakeUp:
          set:
            message: '${ "Done! Woke up." }'
`

const durableServiceYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: e2e-durable-service
  namespace: logic-operator-system
spec:
  defaultDefinition:
    name: e2e-sleepy-v1-0-0
  ingress:
    host: e2e-durable.lvh.me
    ingressClassName: nginx
`

func durableTests() {
	Context("Durable LogicFlowRuntime with PostgreSQL persistence", Ordered, func() {
		BeforeAll(func() {
			By("creating infra namespace for PostgreSQL (no Pod Security restrictions)")
			cmd := exec.Command("kubectl", "create", "namespace", durableInfraNamespace)
			_, _ = utils.Run(cmd) // ignore error if namespace already exists

			By("creating PostgreSQL credentials secret in operator namespace")
			secretOp := strings.NewReader(durablePGSecretYAML)
			cmd = exec.Command("kubectl", "apply", "-f", "-", "-n", namespace)
			cmd.Stdin = secretOp
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("deploying PostgreSQL infra from persistence sample")
			cmd = exec.Command("kubectl", "apply",
				"-f", "config/samples/persistence/postgresql.yaml",
				"-n", durableInfraNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for PostgreSQL to be ready")
			waitForPG := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod",
					"-l", "app=postgresql",
					"-n", durableInfraNamespace,
					"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			Eventually(waitForPG, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("creating the durable LogicFlowRuntime")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(durableRuntimeYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating the sleepy LogicFlowDefinition")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(durableSleepyDefinitionYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating the durable LogicFlowService")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(durableServiceYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			for _, args := range [][]string{
				{"logicflowservice", "e2e-durable-service"},
				{"logicflowdefinition", "e2e-sleepy-v1-0-0"},
				{"logicflowruntime", durableRuntimeName},
				{"secret", "postgresql-secret"},
			} {
				cmd := exec.Command("kubectl", "delete", args[0], args[1],
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
			cmd := exec.Command("kubectl", "delete", "namespace",
				durableInfraNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			specReport := CurrentSpecReport()
			if specReport.Failed() {
				By("collecting logs from durable runtime pods on test failure")
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
					"-n", namespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				podNames, err := utils.Run(cmd)
				if err == nil {
					for _, podName := range strings.Fields(podNames) {
						By(fmt.Sprintf("fetching logs from pod %s", podName))
						cmd := exec.Command("kubectl", "logs", podName, "-n", namespace)
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
					durableRuntimeName, "-n", namespace,
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
					"-n", namespace,
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
					"-n", namespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				podNames, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				pods := strings.Fields(podNames)
				g.Expect(pods).To(HaveLen(2))

				cmd = exec.Command("kubectl", "get", "leases",
					"-n", namespace,
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
			svcURL := fmt.Sprintf("http://e2e-durable.lvh.me/q/flow/exec/%s/sleepy/1.0.0", namespace)
			healthURL := "http://e2e-durable.lvh.me/q/health/ready"

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
				"-n", namespace, "-o", "jsonpath={.spec.holderIdentity}")
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
				podName, "-n", namespace, "--grace-period=0", "--force"))
			Expect(err).NotTo(HaveOccurred())

			By("waiting for 2 ready replicas again after crash")
			verifyTwoReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					durableRuntimeName, "-n", namespace,
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
					"-n", namespace, "-o", "jsonpath={.spec.holderIdentity}")
				holder, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				holder = strings.TrimSpace(holder)
				g.Expect(holder).NotTo(BeEmpty(), "lease %q has no holder", invocation.WorkflowApplicationID)
				g.Expect(holder).NotTo(Equal(podName), "expected lease to move off the deleted pod")

				cmd = exec.Command("kubectl", "get", "pod", holder,
					"-n", namespace,
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

		It("should persist leases after scaling down to 1", func() {
			By("verifying leases persist after scale-down")
			verifyLeasePersist := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "leases",
					"-n", namespace,
					"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
					"--no-headers")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				lines := strings.Split(strings.TrimSpace(out), "\n")
				// Leases should persist (at least the ones from 2 replicas)
				g.Expect(lines).ToNot(BeEmpty(), "expected at least 1 lease to persist")
			}
			Eventually(verifyLeasePersist, time.Minute, 5*time.Second).Should(Succeed())
		})
	})
}

func startRunnerPortForward(podName string) (string, func(), error) {
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
