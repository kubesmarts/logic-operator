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

// Multi-version rollout fixtures: two definitions of the same workflow ("payment")
// sharing one runtime, differing only by version. This mirrors a real canary rollout.
const (
	mvRuntimeName = "e2e-mv-rt"
	mvDefV1Name   = "e2e-mv-def-v1"
	mvDefV2Name   = "e2e-mv-def-v2"
)

const mvRuntimeYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: e2e-mv-rt
  namespace: %s
spec: {}
`

// mvDefinitionYAML is parameterized by: name, namespace, version.
const mvDefinitionYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: %s
  namespace: %s
spec:
  runtimeRef:
    name: e2e-mv-rt
  flow:
    document:
      dsl: "1.0.0"
      namespace: payments
      name: payment
      version: "%s"
    do:
      - step1:
          set:
            result: ok
`

func logicFlowServiceMultiVersionTests() {
	Context("LogicFlowService multi-version traffic splitting", Ordered, func() {
		const svcName = "e2e-mv-svc"
		const host = "payments-mv.example.com"

		// rewritePath builds the expected rewrite-target for a given workflow version.
		rewritePath := func(version string) string {
			return "/q/flow/exec/" + namespace + "/payment/" + version
		}

		getAnnotation := func(g Gomega, kind, name, annotation string) string {
			cmd := exec.Command("kubectl", "get", kind, name, "-n", namespace,
				"-o", "jsonpath={.metadata.annotations."+annotation+"}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			return output
		}

		BeforeAll(func() {
			By("creating the shared LogicFlowRuntime")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(mvRuntimeYAML, namespace))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowRuntime")

			By("waiting for the runtime to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime",
					mvRuntimeName, "-n", namespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Ready"))
			}, 3*time.Minute).Should(Succeed())

			By("creating the v1.0.0 definition")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(mvDefinitionYAML, mvDefV1Name, namespace, "1.0.0"))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create v1 LogicFlowDefinition")

			By("creating the v1.1.0 definition")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(mvDefinitionYAML, mvDefV2Name, namespace, "1.1.0"))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create v2 LogicFlowDefinition")
		})

		AfterAll(func() {
			for _, name := range []string{svcName} {
				cmd := exec.Command("kubectl", "delete", "logicflowservice",
					name, "-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
			for _, name := range []string{mvDefV1Name, mvDefV2Name} {
				cmd := exec.Command("kubectl", "delete", "logicflowdefinition",
					name, "-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
			cmd := exec.Command("kubectl", "delete", "logicflowruntime",
				mvRuntimeName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should serve v1 only when configured as the default definition", func() {
			svcYAML := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: %s
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress:
    host: %s`

			By("creating the service pointing at v1")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(svcYAML, svcName, namespace, mvDefV1Name, host))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowService")

			By("verifying the primary Ingress rewrites to v1.0.0")
			Eventually(func(g Gomega) {
				g.Expect(getAnnotation(g, "ingress", svcName,
					`nginx\.ingress\.kubernetes\.io/rewrite-target`)).To(Equal(rewritePath("1.0.0")))
			}).Should(Succeed())

			By("verifying no canary Ingress exists")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "ingress", svcName+"-canary", "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "canary Ingress should not exist for a default definition")
			}, 5*time.Second).Should(Succeed())
		})

		It("should create a canary Ingress when v2 is introduced at 20%", func() {
			svcYAML := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: %s
  namespace: %s
spec:
  traffic:
    - definitionRef:
        name: %s
      weight: 80
    - definitionRef:
        name: %s
      weight: 20
  ingress:
    host: %s`

			By("updating the service to split traffic 80/20 (v1/v2)")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(svcYAML, svcName, namespace, mvDefV1Name, mvDefV2Name, host))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to update LogicFlowService for canary")

			By("verifying the primary Ingress still rewrites to v1.0.0 (higher weight)")
			Eventually(func(g Gomega) {
				g.Expect(getAnnotation(g, "ingress", svcName,
					`nginx\.ingress\.kubernetes\.io/rewrite-target`)).To(Equal(rewritePath("1.0.0")))
			}).Should(Succeed())

			By("verifying the canary Ingress rewrites to v1.1.0 with 20% weight")
			Eventually(func(g Gomega) {
				g.Expect(getAnnotation(g, "ingress", svcName+"-canary",
					`nginx\.ingress\.kubernetes\.io/canary`)).To(Equal("true"))
				g.Expect(getAnnotation(g, "ingress", svcName+"-canary",
					`nginx\.ingress\.kubernetes\.io/canary-weight`)).To(Equal("20"))
				g.Expect(getAnnotation(g, "ingress", svcName+"-canary",
					`nginx\.ingress\.kubernetes\.io/rewrite-target`)).To(Equal(rewritePath("1.1.0")))
			}).Should(Succeed())

			By("verifying the direct-version Ingress uses a regex path")
			Eventually(func(g Gomega) {
				g.Expect(getAnnotation(g, "ingress", svcName+"-direct",
					`nginx\.ingress\.kubernetes\.io/use-regex`)).To(Equal("true"))
				cmd := exec.Command("kubectl", "get", "ingress", svcName+"-direct", "-n", namespace,
					"-o", "jsonpath={.spec.rules[0].http.paths[0].path}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("/v/(.+)"))
			}).Should(Succeed())

			By("verifying status reflects the 80/20 split")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowservice", svcName, "-n", namespace,
					"-o", "jsonpath={.status.traffic[*].weight}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("80 20"))
			}).Should(Succeed())
		})

		It("should remove canary Ingresses when v2 is promoted to default", func() {
			svcYAML := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowService
metadata:
  name: %s
  namespace: %s
spec:
  defaultDefinition:
    name: %s
  ingress:
    host: %s`

			By("promoting v2 to the default definition")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(svcYAML, svcName, namespace, mvDefV2Name, host))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to promote v2")

			By("verifying the primary Ingress now rewrites to v1.1.0")
			Eventually(func(g Gomega) {
				g.Expect(getAnnotation(g, "ingress", svcName,
					`nginx\.ingress\.kubernetes\.io/rewrite-target`)).To(Equal(rewritePath("1.1.0")))
			}).Should(Succeed())

			By("verifying the canary and direct Ingresses are cleaned up")
			Eventually(func(g Gomega) {
				for _, suffix := range []string{"-canary", "-direct"} {
					cmd := exec.Command("kubectl", "get", "ingress", svcName+suffix, "-n", namespace)
					_, err := utils.Run(cmd)
					g.Expect(err).To(HaveOccurred(), "Ingress %s should be deleted after promotion", svcName+suffix)
				}
			}).Should(Succeed())
		})

		It("should keep serving v2 after v1 is decommissioned", func() {
			By("deleting the v1 definition")
			cmd := exec.Command("kubectl", "delete", "logicflowdefinition",
				mvDefV1Name, "-n", namespace, "--wait=true", "--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the v1 ConfigMap is garbage collected")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap", "lfd-"+mvDefV1Name, "-n", namespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "v1 ConfigMap should be garbage collected")
			}).Should(Succeed())

			By("verifying the service still rewrites to v1.1.0")
			Consistently(func(g Gomega) {
				g.Expect(getAnnotation(g, "ingress", svcName,
					`nginx\.ingress\.kubernetes\.io/rewrite-target`)).To(Equal(rewritePath("1.1.0")))
			}, 5*time.Second).Should(Succeed())
		})
	})
}
