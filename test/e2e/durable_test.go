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

// durableRuntimeName uses a distinct name to avoid collision with demo samples.
const durableRuntimeName = "e2e-durable-rt"

// durableInfraNamespace is a separate namespace for PostgreSQL infra.
// logic-operator-system enforces the restricted Pod Security Standard which
// rejects the postgres:16-alpine image (runs as root). A separate namespace
// without that restriction is used instead.
const durableInfraNamespace = "e2e-durable-infra"

// durableRuntimeYAML — spec.image intentionally omitted so the operator defaults to
// QuarkusFlowVersion + "standard" variant from internal/controller/quarkus_constants.go.
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

// durablePGSecretYAML creates the credentials secret in logic-operator-system so the
// runtime's Deployment can reference it via secretRef. The PostgreSQL Deployment itself
// runs in durableInfraNamespace (no Pod Security restrictions).
const durablePGSecretYAML = `
apiVersion: v1
kind: Secret
metadata:
  name: postgresql-secret
  namespace: logic-operator-system
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

func durableTests() {
	Context("Durable LogicFlowRuntime with PostgreSQL persistence", Ordered, func() {
		BeforeAll(func() {
			By("creating infra namespace for PostgreSQL (no Pod Security restrictions)")
			cmd := exec.Command("kubectl", "create", "namespace", durableInfraNamespace)
			_, _ = utils.Run(cmd) // ignore error if namespace already exists

			By("creating PostgreSQL credentials secret in operator namespace")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(durablePGSecretYAML)
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
		})

		AfterAll(func() {
			for _, args := range [][]string{
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
			for _, pod := range []string{"curl-durable-start", "curl-durable-check"} {
				cmd := exec.Command("kubectl", "delete", "pod", pod,
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
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
			svcURL := fmt.Sprintf("http://%s.%s.svc:80/q/flow/exec/%s/sleepy/1.0.0",
				durableRuntimeName, namespace, namespace)
			metricsURL := fmt.Sprintf("http://%s.%s.svc:80/q/metrics",
				durableRuntimeName, namespace)

			By("starting a sleepy workflow instance without retries — kill pod while it sleeps")
			startArgs := fmt.Sprintf(
				`curl -s -X POST -H 'Content-Type: application/json' -d '{"name":"e2e"}' %s`, svcURL)
			_, err := utils.RunCurlPod("curl-durable-start", namespace, startArgs)
			Expect(err).NotTo(HaveOccurred())

			waitForStart := func(g Gomega) {
				c := exec.Command("kubectl", "get", "pod", "curl-durable-start",
					"-n", namespace, "-o", "jsonpath={.status.phase}")
				out, err := utils.Run(c)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Succeeded"))
			}
			Eventually(waitForStart, time.Minute, 3*time.Second).Should(Succeed())

			startOutput, err := utils.Run(
				exec.Command("kubectl", "logs", "curl-durable-start", "-n", namespace))
			Expect(err).NotTo(HaveOccurred())
			Expect(startOutput).To(ContainSubstring("instanceId"),
				"expected workflow invocation to return an instanceId")

			By("killing one pod immediately — workflow is still sleeping")
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
				_ = exec.Command("kubectl", "delete", "pod", "curl-durable-check",
					"-n", namespace, "--ignore-not-found").Run()
				metricArgs := fmt.Sprintf(`curl -s %s`, metricsURL)
				_, err := utils.RunCurlPod("curl-durable-check", namespace, metricArgs)
				g.Expect(err).NotTo(HaveOccurred())

				waitPod := func(g2 Gomega) {
					out, err := utils.Run(exec.Command("kubectl", "get", "pod", "curl-durable-check",
						"-n", namespace, "-o", "jsonpath={.status.phase}"))
					g2.Expect(err).NotTo(HaveOccurred())
					g2.Expect(out).To(Equal("Succeeded"))
				}
				Eventually(waitPod, 30*time.Second, 2*time.Second).Should(Succeed())

				metricsOutput, err := utils.Run(
					exec.Command("kubectl", "logs", "curl-durable-check", "-n", namespace))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(metricsOutput).To(ContainSubstring(
					`quarkus_flow_workflow_completed_total{workflow="sleepy"`),
					"expected sleepy workflow completion metric on at least one runtime pod")
			}
			Eventually(verifyCompleted, 3*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should re-acquire its lease after pod is deleted", func() {
			cmd := exec.Command("kubectl", "get", "pods",
				"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
				"-n", namespace,
				"-o", "jsonpath={.items[0].metadata.name}")
			podName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			podName = strings.TrimSpace(podName)

			By(fmt.Sprintf("deleting pod %s", podName))
			cmd = exec.Command("kubectl", "delete", "pod", podName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for 2 ready replicas again")
			verifyTwoReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					durableRuntimeName, "-n", namespace,
					"-o", "jsonpath={.status.availableReplicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("2"))
			}
			Eventually(verifyTwoReady, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying both leases are held by current running pods")
			verifyHolders := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
					"-n", namespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				podNames, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				cmd = exec.Command("kubectl", "get", "leases",
					"-n", namespace,
					"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
					"-o", "jsonpath={.items[*].spec.holderIdentity}")
				holders, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				pods := strings.Fields(podNames)
				g.Expect(strings.Fields(holders)).To(HaveLen(len(pods)))
				for _, h := range strings.Fields(holders) {
					g.Expect(pods).To(ContainElement(h))
				}
			}
			Eventually(verifyHolders, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should remove excess lease when scaling down to 1", func() {
			By("scaling down to 1 replica")
			cmd := exec.Command("kubectl", "patch", "logicflowruntime", durableRuntimeName,
				"-n", namespace,
				"--type=merge", "-p", `{"spec":{"replicas":1}}`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			verifyOneReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					durableRuntimeName, "-n", namespace,
					"-o", "jsonpath={.status.availableReplicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("1"))
			}
			Eventually(verifyOneReady, 2*time.Minute, 5*time.Second).Should(Succeed())

			verifyOneLease := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "leases",
					"-n", namespace,
					"-l", fmt.Sprintf("%s=%s", controller.LabelDurablePool, durableRuntimeName),
					"--no-headers")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "expected 1 lease to remain")
				lines := strings.Split(strings.TrimSpace(out), "\n")
				g.Expect(lines).To(HaveLen(1))
			}
			Eventually(verifyOneLease, time.Minute, 5*time.Second).Should(Succeed())
		})
	})
}
