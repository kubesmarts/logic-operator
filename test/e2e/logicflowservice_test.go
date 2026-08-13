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

const testServiceRuntimeName = "e2e-svc-rt"
const testServiceRuntimeYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: e2e-svc-rt
  namespace: %s
spec: {}
`

const testServiceDefinitionName = "e2e-svc-def"
const testServiceDefinitionYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: e2e-svc-def
  namespace: %s
spec:
  runtimeRef:
    name: e2e-svc-rt
  flow:
    document:
      dsl: "1.0.0"
      namespace: payments
      name: payment
      version: "1.0.0"
    do:
      - step1:
          set:
            result: ok
`

func logicFlowServiceTests() {
	Context("LogicFlowService lifecycle", Ordered, func() {
		const svcName = "e2e-payment-svc"

		BeforeAll(func() {
			By("creating the prerequisite LogicFlowRuntime")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(testServiceRuntimeYAML, namespace))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowRuntime")

			By("waiting for the runtime to be ready")
			verifyRT := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime",
					testServiceRuntimeName, "-n", namespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Ready"))
			}
			Eventually(verifyRT, 3*time.Minute).Should(Succeed())

			By("creating the prerequisite LogicFlowDefinition")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(testServiceDefinitionYAML, namespace))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowDefinition")
		})

		AfterAll(func() {
			By("deleting the LogicFlowService")
			cmd := exec.Command("kubectl", "delete", "logicflowservice",
				svcName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting the LogicFlowDefinition")
			cmd = exec.Command("kubectl", "delete", "logicflowdefinition",
				testServiceDefinitionName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting the LogicFlowRuntime")
			cmd = exec.Command("kubectl", "delete", "logicflowruntime",
				testServiceRuntimeName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should create an Ingress with nginx rewrite-target for default definition", func() {
			svcYAML := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: %s
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress:
    host: payments.example.com`

			By("creating the LogicFlowService")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(svcYAML, svcName, namespace, testServiceDefinitionName))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowService")

			By("verifying the Ingress is created with correct rewrite-target")
			verifyIngress := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ingress",
					svcName, "-n", namespace,
					"-o", "jsonpath={.metadata.annotations.nginx\\.ingress\\.kubernetes\\.io/rewrite-target}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("/q/flow/exec/" + namespace + "/payment/1.0.0"))
			}
			Eventually(verifyIngress).Should(Succeed())

			By("verifying the Ingress host")
			cmd = exec.Command("kubectl", "get", "ingress",
				svcName, "-n", namespace,
				"-o", "jsonpath={.spec.rules[0].host}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("payments.example.com"))

			By("verifying the Ingress path is /")
			cmd = exec.Command("kubectl", "get", "ingress",
				svcName, "-n", namespace,
				"-o", "jsonpath={.spec.rules[0].http.paths[0].path}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("/"))

			By("verifying the backend points to the runtime service")
			cmd = exec.Command("kubectl", "get", "ingress",
				svcName, "-n", namespace,
				"-o", "jsonpath={.spec.rules[0].http.paths[0].backend.service.name}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal(testServiceRuntimeName))
		})

		It("should populate status fields after reconciliation", func() {
			verifyStatus := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowservice",
					svcName, "-n", namespace,
					"-o", "jsonpath={.status.runtimeRef.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(testServiceRuntimeName))
			}
			Eventually(verifyStatus).Should(Succeed())

			By("verifying traffic status shows 100% to the definition")
			cmd := exec.Command("kubectl", "get", "logicflowservice",
				svcName, "-n", namespace,
				"-o", "jsonpath={.status.traffic[0].weight}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("100"))

			By("verifying ingressRef is set")
			cmd = exec.Command("kubectl", "get", "logicflowservice",
				svcName, "-n", namespace,
				"-o", "jsonpath={.status.ingressRef.name}")
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal(svcName))
		})

		It("should clean up the Ingress on deletion", func() {
			By("deleting the LogicFlowService")
			cmd := exec.Command("kubectl", "delete", "logicflowservice",
				svcName, "-n", namespace, "--wait=true", "--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Ingress is garbage collected")
			verifyGC := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ingress",
					svcName, "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Ingress should be deleted")
			}
			Eventually(verifyGC).Should(Succeed())
		})
	})

	Context("LogicFlowService webhook validation", Ordered, func() {
		const webhookDefName = "e2e-webhook-def"
		const webhookRTName = "e2e-webhook-rt"

		BeforeAll(func() {
			By("creating a runtime for webhook tests")
			rtYAML := fmt.Sprintf(`apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: %s
  namespace: %s
spec: {}`, webhookRTName, namespace)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(rtYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a definition for webhook tests")
			defYAML := fmt.Sprintf(`apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: %s
  namespace: %s
spec:
  runtimeRef:
    name: %s
  flow:
    document:
      dsl: "1.0.0"
      namespace: test
      name: webhook-test
      version: "1.0.0"
    do:
      - step1:
          set:
            result: ok`, webhookDefName, namespace, webhookRTName)
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(defYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			cmd := exec.Command("kubectl", "delete", "logicflowdefinition",
				webhookDefName, "-n", namespace, "--ignore-not-found")
			_ = cmd.Run()
			cmd = exec.Command("kubectl", "delete", "logicflowruntime",
				webhookRTName, "-n", namespace, "--ignore-not-found")
			_ = cmd.Run()
		})

		It("should reject a service without host in nginx mode", func() {
			noHostSvc := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: no-host-svc
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress: {}`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(noHostSvc, namespace, webhookDefName))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected rejection for missing host")
			Expect(string(output)).To(ContainSubstring("host"))
		})

		It("should reject a service without traffic or defaultDefinition", func() {
			noDefSvc := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: no-def-svc
  namespace: %s
spec:
  ingress:
    host: test.example.com`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(noDefSvc, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected rejection for missing definition")
			Expect(string(output)).To(ContainSubstring("defaultDefinition"))
		})

		It("should reject a non-nginx ingressClassName", func() {
			wrongClassSvc := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: wrong-class-svc
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress:
    host: test.example.com
    ingressClassName: traefik`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(wrongClassSvc, namespace, webhookDefName))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected rejection for non-nginx className")
			Expect(string(output)).To(ContainSubstring("nginx"))
		})

		It("should reject adding gatewayRef on update", func() {
			validSvc := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: immutable-gw-svc
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress:
    host: test.example.com`

			By("creating a service without gatewayRef")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(validSvc, namespace, webhookDefName))
			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "expected valid service to be created: %s", string(output))

			By("trying to add gatewayRef")
			withGWSvc := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: immutable-gw-svc
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress:
    host: test.example.com
    gatewayRef:
      name: some-gateway`

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(withGWSvc, namespace, webhookDefName))
			output, err = cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected gatewayRef change to be rejected")
			Expect(string(output)).To(ContainSubstring("gatewayRef"))

			By("cleaning up")
			cmd = exec.Command("kubectl", "delete", "logicflowservice",
				"immutable-gw-svc", "-n", namespace, "--ignore-not-found")
			_ = cmd.Run()
		})
	})
}
