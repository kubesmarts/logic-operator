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

const platformName = "flow-platform"
const dataIndexName = "flow-platform-data-index"
const runtimeName = "hello-runtime"

// dataIndexIntegrationWorkflowYAML - test-specific workflow for DataIndex integration verification
const dataIndexIntegrationWorkflowYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: e2e-dataindex-integration-wf
  namespace: logic-operator-system
spec:
  runtimeRef:
    name: hello-runtime
  flow:
    document:
      dsl: "1.0.0"
      namespace: logic-operator-system
      name: dataindex-test
      version: "1.0.0"
    do:
      - log:
          set:
            result: '${ "DataIndex Integration Test completed for " + .name }'
`

func platformTests() {
	Context("LogicPlatform with DataIndex", Ordered, func() {
		BeforeAll(func() {
			By("creating infra namespace for PostgreSQL (if not exists)")
			cmd := exec.Command("kubectl", "create", "namespace", durableInfraNamespace)
			_, _ = utils.Run(cmd) // ignore error if namespace already exists

			By("applying all resources from config/samples/persistence/")
			cmd = exec.Command("kubectl", "apply",
				"-f", "config/samples/persistence/postgresql.yaml",
				"-f", "config/samples/persistence/logic_v1_logicplatform.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowruntime.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowdefinition_sleepy.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowservice_sleepy.yaml",
				"-n", namespace)
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

			By("waiting for LogicPlatform to be deployed")
			waitForPlatform := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicplatform", platformName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(platformName))
			}
			Eventually(waitForPlatform, 30*time.Second, 2*time.Second).Should(Succeed())

			By("waiting for LogicFlowRuntime to be deployed")
			waitForRuntime := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime", runtimeName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(runtimeName))
			}
			Eventually(waitForRuntime, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		AfterAll(func() {
			By("deleting all resources from config/samples/persistence/")
			cmd := exec.Command("kubectl", "delete",
				"-f", "config/samples/persistence/postgresql.yaml",
				"-f", "config/samples/persistence/logic_v1_logicplatform.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowruntime.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowdefinition_sleepy.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowservice_sleepy.yaml",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should deploy DataIndex deployment", func() {
			By("waiting for DataIndex deployment to be created")
			waitForDeployment := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", dataIndexName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(dataIndexName))
			}
			Eventually(waitForDeployment, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying deployment has correct image")
			cmd := exec.Command("kubectl", "get", "deployment", dataIndexName,
				"-n", namespace,
				"-o", "jsonpath={.spec.template.spec.containers[0].image}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("data-index-service"))
			Expect(out).To(ContainSubstring("postgresql"))

			By("verifying deployment has default resources")
			cmd = exec.Command("kubectl", "get", "deployment", dataIndexName,
				"-n", namespace,
				"-o", "jsonpath={.spec.template.spec.containers[0].resources}")
			out, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("250m"))  // CPU request
			Expect(out).To(ContainSubstring("512Mi")) // Memory request
		})

		It("should create DataIndex service", func() {
			By("waiting for DataIndex service to be created")
			waitForService := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", dataIndexName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(dataIndexName))
			}
			Eventually(waitForService, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying service has correct ports")
			cmd := exec.Command("kubectl", "get", "service", dataIndexName,
				"-n", namespace,
				"-o", "jsonpath={.spec.ports[0].port}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("80"))
		})

		It("should have DataIndex pod ready", func() {
			By("waiting for DataIndex pod to be ready")
			waitForPod := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod",
					"-l", "app.kubernetes.io/name="+dataIndexName,
					"-n", namespace,
					"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			// DataIndex needs time to connect to PostgreSQL and start
			Eventually(waitForPod, 5*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should update LogicPlatform status", func() {
			By("verifying status has deployment available condition")
			waitForStatus := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicplatform", platformName,
					"-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='DataIndexDeploymentAvailable')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			Eventually(waitForStatus, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying status has service ready condition")
			cmd := exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.conditions[?(@.type=='DataIndexServiceReady')].status}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("True"))

			By("verifying status has persistence config valid")
			cmd = exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.conditions[?(@.type=='DataIndexPersistenceReady')].status}")
			out, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("True"))

			By("verifying status has GraphQL endpoint")
			cmd = exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.dataIndex.service.graphqlEndpoint}")
			out, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("/graphql"))

			By("verifying status phase is Ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicplatform", platformName,
					"-n", namespace,
					"-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Ready"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should have working GraphQL endpoint", func() {
			By("getting the GraphQL endpoint from status")
			cmd := exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.dataIndex.service.graphqlEndpoint}")
			endpoint, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint).NotTo(BeEmpty())

			By("creating a curl pod to test GraphQL endpoint")
			curlPod := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: curl-graphql-test
  namespace: %s
spec:
  restartPolicy: Never
  containers:
  - name: curl
    image: curlimages/curl:latest
    command:
    - /bin/sh
    - -c
    - |
      curl -s -X POST %s \
        -H "Content-Type: application/json" \
        -d '{"query":"{ __schema { queryType { name } } }"}'
`, namespace, endpoint)

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(curlPod)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for curl pod to complete")
			waitForCurl := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", "curl-graphql-test",
					"-n", namespace,
					"-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Succeeded"))
			}
			Eventually(waitForCurl, 1*time.Minute, 5*time.Second).Should(Succeed())

			By("checking GraphQL response")
			cmd = exec.Command("kubectl", "logs", "curl-graphql-test", "-n", namespace)
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("Query")) // GraphQL schema introspection response

			By("cleaning up curl pod")
			cmd = exec.Command("kubectl", "delete", "pod", "curl-graphql-test",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should capture workflow data in DataIndex via structured logging", func() {
			By("creating the workflow definition for DataIndex integration test")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(dataIndexIntegrationWorkflowYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for workflow to be registered in runtime")
			waitForWorkflow := func(g Gomega) {
				c := exec.Command("kubectl", "exec", "-n", namespace,
					fmt.Sprintf("deployment/%s", runtimeName), "--",
					"sh", "-c", "curl -s http://localhost:8080/q/flow/definitions")
				out, err := utils.Run(c)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("dataindex-test"))
			}
			Eventually(waitForWorkflow, 3*time.Minute, 10*time.Second).Should(Succeed())

			By("verifying databaseConnected is true in platform status")
			cmd = exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.dataIndex.persistence.databaseConnected}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("true"))

			By("executing workflow via runtime REST API")
			execWorkflow := func(g Gomega) {
				execCmd := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/json" -d '{"name":"test"}' http://localhost:8080/q/flow/exec/%s/dataindex-test/1.0.0`, namespace)
				cmd := exec.Command("kubectl", "exec", "-n", namespace,
					fmt.Sprintf("deployment/%s", runtimeName), "--", "sh", "-c", execCmd)
				workflowResponse, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(workflowResponse).To(ContainSubstring("instanceId"))
			}
			Eventually(execWorkflow, 1*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for workflow data to appear in DataIndex")
			graphqlEndpoint, err := utils.Run(exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.dataIndex.service.graphqlEndpoint}"))
			Expect(err).NotTo(HaveOccurred())

			graphqlQuery := `{"query":"{ getWorkflowInstances { id status outputData } }"}`

			waitForWorkflowData := func(g Gomega) {
				graphqlCurl := fmt.Sprintf(
					`curl -s -X POST -H 'Content-Type: application/json' -d '%s' %s`,
					graphqlQuery, graphqlEndpoint)

				podName := fmt.Sprintf("curl-workflow-data-%d", time.Now().UnixNano()%10000)
				_, err := utils.RunCurlPod(podName, namespace, graphqlCurl)
				g.Expect(err).NotTo(HaveOccurred())

				waitForPod := func(g Gomega) {
					c := exec.Command("kubectl", "get", "pod", podName,
						"-n", namespace,
						"-o", "jsonpath={.status.phase}")
					out, err := utils.Run(c)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(Equal("Succeeded"))
				}
				Eventually(waitForPod, 30*time.Second, 5*time.Second).Should(Succeed())

				graphqlResponse, err := utils.Run(
					exec.Command("kubectl", "logs", podName, "-n", namespace))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(graphqlResponse).To(ContainSubstring("getWorkflowInstances"))
				g.Expect(graphqlResponse).To(ContainSubstring("COMPLETED"))
				g.Expect(graphqlResponse).NotTo(ContainSubstring("outputData\":null"))

				// Cleanup this curl pod
				cmd := exec.Command("kubectl", "delete", "pod", podName,
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			}
			Eventually(waitForWorkflowData, 3*time.Minute, 10*time.Second).Should(Succeed())

			By("cleaning up workflow definition")
			cmd = exec.Command("kubectl", "delete", "logicflowdefinition", "e2e-dataindex-integration-wf",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})
	})
}
