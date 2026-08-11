package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubesmarts/logic-operator/test/utils"
)

const testRuntimeName = "e2e-minimal-rt"
const testRuntimeYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: e2e-minimal-rt
  namespace: logic-operator-system
spec: {}
`

const testDefinitionName = "e2e-hello-world"
const testDefinitionYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: e2e-hello-world
  namespace: logic-operator-system
spec:
  runtimeRef:
    name: e2e-minimal-rt
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: hello-world
      version: "1.0.0"
    do:
      - greet:
          set:
            message: '${ "Hello, " + .name + "!" }'
`

func logicFlowRuntimeLifecycleTests() {
	Context("LogicFlowRuntime minimal lifecycle", Ordered, func() {
		BeforeAll(func() {
			By("applying a minimal LogicFlowRuntime CR")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testRuntimeYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowRuntime")
		})

		AfterAll(func() {
			By("deleting the LogicFlowDefinition CR")
			cmd := exec.Command("kubectl", "delete", "logicflowdefinition",
				testDefinitionName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting the LogicFlowRuntime CR")
			cmd = exec.Command("kubectl", "delete", "logicflowruntime",
				testRuntimeName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the curl-workflow pod")
			cmd = exec.Command("kubectl", "delete", "pod", "curl-workflow",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should create a Deployment for the runtime", func() {
			verifyDeployment := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					testRuntimeName, "-n", namespace,
					"-o", "jsonpath={.status.availableReplicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"), "Deployment should have 1 available replica")
			}
			Eventually(verifyDeployment, 3*time.Minute).Should(Succeed())
		})

		It("should create a Service for the runtime", func() {
			cmd := exec.Command("kubectl", "get", "service",
				testRuntimeName, "-n", namespace,
				"-o", "jsonpath={.spec.ports[0].port}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("80"), "Service should expose port 80")
		})

		It("should have a Running pod with the minimal runner image", func() {
			verifyPod := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", testRuntimeName),
					"-n", namespace,
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}
			Eventually(verifyPod).Should(Succeed())

			cmd := exec.Command("kubectl", "get", "pods",
				"-l", fmt.Sprintf("app.kubernetes.io/name=%s", testRuntimeName),
				"-n", namespace,
				"-o", "jsonpath={.items[0].spec.containers[0].image}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("quarkus-flow-runner"))
			Expect(output).To(ContainSubstring("minimal"))
		})

		It("should reach Ready phase in status", func() {
			verifyStatus := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime",
					testRuntimeName, "-n", namespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Ready"))
			}
			Eventually(verifyStatus, 3*time.Minute).Should(Succeed())
		})

		It("should mount a LogicFlowDefinition's ConfigMap after definition is applied", func() {
			By("applying a LogicFlowDefinition CR")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testDefinitionYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowDefinition")

			By("verifying the ConfigMap is created")
			verifyCM := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap",
					"-l", fmt.Sprintf("logic.kubesmarts.org/runtime-ref=%s", testRuntimeName),
					"-n", namespace,
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "ConfigMap should exist")
			}
			Eventually(verifyCM).Should(Succeed())

			By("waiting for the Deployment rollout with the ConfigMap volume")
			verifyVolume := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					testRuntimeName, "-n", namespace,
					"-o", "jsonpath={.spec.template.spec.volumes[*].name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("lfd-"), "ConfigMap volume should be mounted")
			}
			Eventually(verifyVolume).Should(Succeed())

			By("waiting for the new pod to be running")
			verifyNewPod := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", testRuntimeName),
					"-n", namespace,
					"-o", "jsonpath={.items[0].status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}
			Eventually(verifyNewPod, 3*time.Minute).Should(Succeed())

			By("dumping runner pod logs for diagnostics")
			cmd = exec.Command("kubectl", "logs",
				"-l", fmt.Sprintf("app.kubernetes.io/name=%s", testRuntimeName),
				"-n", namespace, "--tail=50")
			logs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Runner pod logs:\n%s\n", logs)
			}
		})

		It("should have the workflow registered in the runner", func() {
			By("querying the definitions API until the workflow is listed")
			verifyDefinition := func(g Gomega) {
				cmd := exec.Command("kubectl", "exec",
					"-n", namespace,
					fmt.Sprintf("deployment/%s", testRuntimeName), "--",
					"curl", "-sf", "http://localhost:8080/q/flow/definitions")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("hello-world"),
					"Workflow should be registered in the runner")
			}
			Eventually(verifyDefinition, 3*time.Minute).Should(Succeed())
		})

		It("should execute the hello-world workflow via the runner API", func() {
			By("creating a curl pod to invoke the workflow")
			curlPodName := "curl-workflow"
			svcURL := fmt.Sprintf("http://%s.%s.svc:80/q/flow/exec/examples/hello-world?wait=true",
				testRuntimeName, namespace)
			curlArgs := "curl -s --retry 5 --retry-delay 3 --retry-all-errors" +
				" -X POST -H 'Content-Type: application/json'" +
				` -d '{"name":"world"}' '` + svcURL + `'`
			overrides := fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [%q],
							"securityContext": {
								"allowPrivilegeEscalation": false,
								"capabilities": {"drop": ["ALL"]},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {"type": "RuntimeDefault"}
							}
						}],
						"restartPolicy": "Never"
					}
				}`, curlArgs)
			cmd := exec.Command("kubectl", "run", curlPodName, "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides", overrides)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-workflow pod")

			By("waiting for the curl pod to complete")
			verifyCurl := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", curlPodName,
					"-o", "jsonpath={.status.phase}", "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod should complete successfully")
			}
			Eventually(verifyCurl, 2*time.Minute).Should(Succeed())

			By("checking the workflow response")
			cmd = exec.Command("kubectl", "logs", curlPodName, "-n", namespace)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to get curl-workflow logs")
			_, _ = fmt.Fprintf(GinkgoWriter, "Workflow response:\n%s\n", output)
			Expect(output).To(ContainSubstring("Hello, world!"), "Workflow output should contain the greeting")
			Expect(output).To(ContainSubstring("COMPLETED"), "Workflow status should be COMPLETED")

			By("cleaning up the curl pod")
			cmd = exec.Command("kubectl", "delete", "pod", curlPodName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should clean up child resources on deletion", func() {
			By("deleting the LogicFlowDefinition CR")
			cmd := exec.Command("kubectl", "delete", "logicflowdefinition",
				testDefinitionName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting the LogicFlowRuntime CR")
			cmd = exec.Command("kubectl", "delete", "logicflowruntime",
				testRuntimeName, "-n", namespace, "--wait=true", "--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete LogicFlowRuntime")

			By("verifying the Deployment is garbage collected")
			verifyGC := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment",
					testRuntimeName, "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Deployment should be deleted")
			}
			Eventually(verifyGC).Should(Succeed())

			By("verifying the Service is garbage collected")
			verifyServiceGC := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service",
					testRuntimeName, "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Service should be deleted")
			}
			Eventually(verifyServiceGC).Should(Succeed())
		})
	})
}
