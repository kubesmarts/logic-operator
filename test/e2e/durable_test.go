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

const durableRuntimeName = "e2e-durable-rt"

// durableRuntimeYAML — spec.image intentionally omitted so the operator defaults to
// QuarkusFlowVersion + "standard" variant from internal/controller/quarkus_constants.go.
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
        databaseName: logicflow
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
      - nap:
          wait:
            seconds: 30
`

func durableTests() {
	Context("Durable LogicFlowRuntime lease coordination", Ordered, func() {
		BeforeAll(func() {
			By("deploying PostgreSQL via kustomize overlay")
			cmd := exec.Command("kubectl", "apply", "-k",
				"config/samples/persistence/e2e-overlay/")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for PostgreSQL to be ready")
			waitForPG := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod",
					"-l", "app=postgresql",
					"-n", namespace,
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
			} {
				cmd := exec.Command("kubectl", "delete", args[0], args[1],
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
			cmd := exec.Command("kubectl", "delete", "-k",
				"config/samples/persistence/e2e-overlay/", "--ignore-not-found")
			_, _ = utils.Run(cmd)
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
				for _, h := range strings.Fields(holders) {
					g.Expect(pods).To(ContainElement(h))
				}
			}
			Eventually(verifyHolders, time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should re-acquire its lease after pod is deleted", func() {
			cmd := exec.Command("kubectl", "get", "pods",
				"-l", fmt.Sprintf("app.kubernetes.io/name=%s", durableRuntimeName),
				"-n", namespace,
				"-o", "jsonpath={.items[0].metadata.name}")
			podName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			podName = strings.TrimSpace(podName)

			By(fmt.Sprintf("deleting pod %s to simulate crash", podName))
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
				lines := strings.Split(strings.TrimSpace(out), "\n")
				g.Expect(lines).To(HaveLen(1))
			}
			Eventually(verifyOneLease, time.Minute, 5*time.Second).Should(Succeed())
		})
	})
}
