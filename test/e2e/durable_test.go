package e2e

import (
	"fmt"
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

		It("should complete a workflow and survive pod crash (verified via metrics)", func() {
			// The operator overrides flow.document.namespace with the CR's Kubernetes namespace.
			// URL format: /q/flow/exec/{document.namespace}/{document.name}/{document.version}
			svcURL := fmt.Sprintf("http://e2e-durable.lvh.me/q/flow/exec/%s/sleepy/1.0.0", namespace)
			metricsURL := "http://e2e-durable.lvh.me/q/metrics"
			healthURL := "http://e2e-durable.lvh.me/q/health/ready"

			By("verifying runtime pods are ready to accept requests")
			verifyHealthy := func(g Gomega) {
				c := exec.Command("curl", "-sf", healthURL)
				out, err := c.CombinedOutput()
				g.Expect(err).NotTo(HaveOccurred(), "health check failed: %s", string(out))
			}
			Eventually(verifyHealthy, 5*time.Minute, 5*time.Second).Should(Succeed())

			By("starting a sleepy workflow instance without retries — kill pod while it sleeps")
			c := exec.Command("curl", "-s", "-X", "POST",
				"-H", "Content-Type: application/json",
				"-d", `{"name":"e2e"}`,
				svcURL)
			out, err := c.CombinedOutput()
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("instanceId"),
				"expected workflow invocation to return an instanceId")

			By("killing pod immediately — workflow is still sleeping")
			podName, err := utils.Run(exec.Command("kubectl", "get", "pods",
				"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
				"-n", namespace, "-o", "jsonpath={.items[0].metadata.name}"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "delete", "pod",
				strings.TrimSpace(podName), "-n", namespace))
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

			By("polling metrics until sleepy workflow shows completed_total >= 1")
			// With 2 replicas the service round-robins. Eventually we hit the pod
			// that processed (and completed) the workflow — whether the original or
			// the resumed one — and its Micrometer counter reflects the completion.
			verifyCompleted := func(g Gomega) {
				c := exec.Command("curl", "-s", metricsURL)
				metricsOutput, err := c.CombinedOutput()
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Contains(string(metricsOutput), `quarkus_flow_workflow_completed_total{workflow="sleepy"`)).
					To(BeTrue(), "expected sleepy workflow completion metric on the runtime pod")
			}
			Eventually(verifyCompleted, 3*time.Minute, 10*time.Second).Should(Succeed())
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
