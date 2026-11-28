package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubesmarts/logic-operator/api/version"
	"github.com/kubesmarts/logic-operator/test/utils"
)

const platformName = "default"
const platformNamespace = "logic-platform"
const dataIndexName = platformName + "-data-index"
const runtimeName = "hello-runtime"
const runtimeReplicaCount = "2"

// Second runtime namespace, used to verify Vector collects logs from several namespaces.
const secondRuntimeNamespace = "logic-runtime-b"
const secondRuntimeName = "hello-runtime-b"
const vectorName = platformName + "-vector"

// secondRuntimeYAML - runtime deployed in a namespace other than the operator one.
const secondRuntimeYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: hello-runtime-b
  namespace: logic-runtime-b
spec:
  replicas: 1
  platformRef:
    name: default
  persistenceMode: PlatformDB
  logging:
    enabled: true
    includeWorkflowPayloads: true
`

// secondRuntimeWorkflowYAML - workflow deployed to the runtime in the second namespace.
const secondRuntimeWorkflowYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: e2e-dataindex-integration-wf-b
  namespace: logic-runtime-b
spec:
  runtimeRef:
    name: hello-runtime-b
  flow:
    document:
      dsl: "1.0.0"
      namespace: logic-runtime-b
      name: dataindex-test-b
      version: "1.0.0"
    do:
      - log:
          set:
            result: '${ "DataIndex multi-namespace test completed for " + .name }'
`

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
	Context("ClusterLogicPlatform with DataIndex", Ordered, func() {
		BeforeAll(func() {
			By("creating ClusterLogicPlatform target namespace and PostgreSQL")
			cmd := exec.Command("kubectl", "create", "namespace", platformNamespace)
			_, _ = utils.Run(cmd) // ignore error if namespace already exists
			cmd = exec.Command("kubectl", "apply",
				"-f", "config/samples/persistence/postgresql.yaml", "-n", platformNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for PostgreSQL to be ready")
			waitForPG := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod",
					"-l", "app=postgresql",
					"-n", platformNamespace,
					"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			Eventually(waitForPG, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("applying the ClusterLogicPlatform and runtime resources")
			cmd = exec.Command("kubectl", "apply",
				"-f", "config/samples/persistence/logic_v1_clusterlogicplatform.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowruntime.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowdefinition_sleepy.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowservice_sleepy.yaml",
				"-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating the second runtime namespace")
			cmd = exec.Command("kubectl", "create", "namespace", secondRuntimeNamespace)
			_, _ = utils.Run(cmd) // ignore error if namespace already exists

			By("configuring Vector to watch the namespaces where this test deploys runtimes")
			cmd = exec.Command("kubectl", "patch", "clusterlogicplatform", platformName,
				"--type=merge",
				"-p", `{"spec":{"vector":{"watchNamespaces":["`+namespace+`","`+secondRuntimeNamespace+`"]}}}`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("deploying the runtime in the second namespace")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(secondRuntimeYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for ClusterLogicPlatform to be deployed")
			waitForPlatform := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
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

			By("waiting for the second LogicFlowRuntime to be deployed")
			waitForSecondRuntime := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime", secondRuntimeName,
					"-n", secondRuntimeNamespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(secondRuntimeName))
			}
			Eventually(waitForSecondRuntime, 30*time.Second, 2*time.Second).Should(Succeed())
		})

		AfterAll(func() {
			By("deleting the runtime resources and ClusterLogicPlatform")
			cmd := exec.Command("kubectl", "delete",
				"-f", "config/samples/persistence/logic_v1_clusterlogicplatform.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowruntime.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowdefinition_sleepy.yaml",
				"-f", "config/samples/persistence/logic_v1_logicflowservice_sleepy.yaml",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			By("waiting for ClusterLogicPlatform cleanup before the next singleton test")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "clusterlogicplatform", platformName)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}).WithTimeout(2 * time.Minute).Should(Succeed())
			cmd = exec.Command("kubectl", "delete", "namespace", platformNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			By("deleting the second runtime namespace")
			cmd = exec.Command("kubectl", "delete", "namespace", secondRuntimeNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should deploy DataIndex deployment", func() {
			By("waiting for DataIndex deployment to be created")
			waitForDeployment := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", dataIndexName,
					"-n", platformNamespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(dataIndexName))
			}
			Eventually(waitForDeployment, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying deployment has correct image")
			cmd := exec.Command("kubectl", "get", "deployment", dataIndexName,
				"-n", platformNamespace,
				"-o", "jsonpath={.spec.template.spec.containers[0].image}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("logic-data-index:" + version.GetDataIndexVersion()))
			Expect(out).To(ContainSubstring("postgresql"))

			By("verifying deployment has default resources")
			cmd = exec.Command("kubectl", "get", "deployment", dataIndexName,
				"-n", platformNamespace,
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
					"-n", platformNamespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(dataIndexName))
			}
			Eventually(waitForService, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying service has correct ports")
			cmd := exec.Command("kubectl", "get", "service", dataIndexName,
				"-n", platformNamespace,
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
					"-n", platformNamespace,
					"-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			// DataIndex needs time to connect to PostgreSQL and start
			Eventually(waitForPod, 5*time.Minute, 10*time.Second).Should(Succeed())
		})

		It("should update ClusterLogicPlatform status", func() {
			By("verifying status has deployment available condition")
			waitForStatus := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
					"-o", "jsonpath={.status.conditions[?(@.type=='DataIndexDeploymentAvailable')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}
			Eventually(waitForStatus, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("verifying status has service ready condition")
			cmd := exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
				"-o", "jsonpath={.status.conditions[?(@.type=='DataIndexServiceReady')].status}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("True"))

			By("verifying status has persistence config valid")
			cmd = exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
				"-o", "jsonpath={.status.conditions[?(@.type=='DataIndexPersistenceReady')].status}")
			out, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("True"))

			By("verifying status has GraphQL endpoint")
			cmd = exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
				"-o", "jsonpath={.status.dataIndex.service.graphqlEndpoint}")
			out, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("/graphql"))

			By("verifying status phase is Ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
					"-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Ready"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should have working GraphQL endpoint", func() {
			By("getting the GraphQL endpoint from status")
			cmd := exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
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
    image: %s
    command:
    - /bin/sh
    - -c
    - |
      curl -s -X POST %s \
        -H "Content-Type: application/json" \
        -d '{"query":"{ __schema { queryType { name } } }"}'
`, namespace, utils.CurlImage(), endpoint)

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(curlPod)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "pod", "curl-graphql-test",
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

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
		})

		It("should capture workflow data in DataIndex via structured logging", func() {
			verifyRuntimeDeploymentReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", runtimeName,
					"-n", namespace,
					"-o", "jsonpath={.status.availableReplicas} {.status.updatedReplicas} {.spec.replicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Fields(out)).To(Equal([]string{runtimeReplicaCount, runtimeReplicaCount, runtimeReplicaCount}))
			}

			By("waiting for both runtime replicas before adding the integration workflow")
			Eventually(verifyRuntimeDeploymentReady, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("creating the workflow definition for DataIndex integration test")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(dataIndexIntegrationWorkflowYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "logicflowdefinition", "e2e-dataindex-integration-wf",
					"-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			By("waiting for the runtime to observe the integration workflow")
			waitForRuntimeDefinition := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime", runtimeName,
					"-n", namespace,
					"-o", `jsonpath={.status.definitions[?(@.name=="dataindex-test")].name}`)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("dataindex-test"))
			}
			Eventually(waitForRuntimeDefinition, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for the runtime Deployment rollout to finish")
			Eventually(verifyRuntimeDeploymentReady, 3*time.Minute, 5*time.Second).Should(Succeed())

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
			cmd = exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
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
			graphqlEndpoint, err := utils.Run(exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
				"-o", "jsonpath={.status.dataIndex.service.graphqlEndpoint}"))
			Expect(err).NotTo(HaveOccurred())

			graphqlQuery := `{"query":"{ getWorkflowInstances { id status outputData } }"}`

			waitForWorkflowData := func(g Gomega) {
				graphqlCurl := fmt.Sprintf(
					`curl -s -X POST -H 'Content-Type: application/json' -d '%s' %s`,
					graphqlQuery, graphqlEndpoint)

				podName := fmt.Sprintf("curl-workflow-data-%d", time.Now().UnixNano())
				defer func() {
					cmd := exec.Command("kubectl", "delete", "pod", podName,
						"-n", namespace, "--ignore-not-found")
					_, _ = utils.Run(cmd)
				}()
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
			}
			Eventually(waitForWorkflowData, 3*time.Minute, 10*time.Second).Should(Succeed())

			By("cleaning up workflow definition")
			cmd = exec.Command("kubectl", "delete", "logicflowdefinition", "e2e-dataindex-integration-wf",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should collect workflow logs from multiple runtime namespaces into the same DataIndex", func() {
			By("verifying Vector receives the comma-separated list of watched namespaces")
			expectedNamespaces := namespace + "," + secondRuntimeNamespace
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", vectorName,
					"-n", platformNamespace,
					"-o", `jsonpath={.spec.template.spec.containers[0].env[?(@.name=="WORKFLOW_NAMESPACES")].value}`)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(expectedNamespaces))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for the Vector rollout to finish")
			cmd := exec.Command("kubectl", "rollout", "status", "daemonset/"+vectorName,
				"-n", platformNamespace, "--timeout=3m")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the runtime in the second namespace to be available")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", secondRuntimeName,
					"-n", secondRuntimeNamespace,
					"-o", "jsonpath={.status.availableReplicas} {.status.updatedReplicas} {.spec.replicas}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Fields(out)).To(Equal([]string{"1", "1", "1"}))
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			By("creating the workflow definition in the second namespace")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(secondRuntimeWorkflowYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "logicflowdefinition", "e2e-dataindex-integration-wf-b",
					"-n", secondRuntimeNamespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			By("waiting for the workflow to be registered in the second runtime")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "logicflowruntime", secondRuntimeName,
					"-n", secondRuntimeNamespace,
					"-o", `jsonpath={.status.definitions[?(@.name=="dataindex-test-b")].name}`)
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("dataindex-test-b"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "exec", "-n", secondRuntimeNamespace,
					"deployment/"+secondRuntimeName, "--",
					"sh", "-c", "curl -s http://localhost:8080/q/flow/definitions")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("dataindex-test-b"))
			}, 3*time.Minute, 10*time.Second).Should(Succeed())

			By("executing a workflow in each runtime namespace")
			execIn := func(runtimeNamespace, runtime, flowName string) {
				Eventually(func(g Gomega) {
					execCmd := fmt.Sprintf(`curl -s -X POST -H "Content-Type: application/json" -d '{"name":"test"}' http://localhost:8080/q/flow/exec/%s/%s/1.0.0`,
						runtimeNamespace, flowName)
					cmd := exec.Command("kubectl", "exec", "-n", runtimeNamespace,
						"deployment/"+runtime, "--", "sh", "-c", execCmd)
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(ContainSubstring("instanceId"))
				}, 1*time.Minute, 5*time.Second).Should(Succeed())
			}
			execIn(namespace, runtimeName, "dataindex-test")
			execIn(secondRuntimeNamespace, secondRuntimeName, "dataindex-test-b")

			By("waiting for workflow instances of both namespaces in the same DataIndex")
			graphqlEndpoint, err := utils.Run(exec.Command("kubectl", "get", "clusterlogicplatform", platformName,
				"-o", "jsonpath={.status.dataIndex.service.graphqlEndpoint}"))
			Expect(err).NotTo(HaveOccurred())

			graphqlQuery := `{"query":"{ getWorkflowInstances { id name namespace status } }"}`
			completed := func(flowName, flowNamespace string) string {
				return fmt.Sprintf(`"name":\s*"%s",\s*"namespace":\s*"%s",\s*"status":\s*"COMPLETED"`, flowName, flowNamespace)
			}

			waitForBothNamespaces := func(g Gomega) {
				graphqlCurl := fmt.Sprintf(
					`curl -s -X POST -H 'Content-Type: application/json' -d '%s' %s`,
					graphqlQuery, graphqlEndpoint)

				podName := fmt.Sprintf("curl-multi-ns-%d", time.Now().UnixNano())
				defer func() {
					cmd := exec.Command("kubectl", "delete", "pod", podName,
						"-n", namespace, "--ignore-not-found")
					_, _ = utils.Run(cmd)
				}()
				_, err := utils.RunCurlPod(podName, namespace, graphqlCurl)
				g.Expect(err).NotTo(HaveOccurred())

				_, err = utils.Run(exec.Command("kubectl", "wait", "--for=jsonpath={.status.phase}=Succeeded",
					"pod/"+podName, "-n", namespace, "--timeout=30s"))
				g.Expect(err).NotTo(HaveOccurred())

				graphqlResponse, err := utils.Run(
					exec.Command("kubectl", "logs", podName, "-n", namespace))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(graphqlResponse).To(MatchRegexp(completed("dataindex-test", namespace)))
				g.Expect(graphqlResponse).To(MatchRegexp(completed("dataindex-test-b", secondRuntimeNamespace)))
			}
			Eventually(waitForBothNamespaces, 3*time.Minute, 10*time.Second).Should(Succeed())
		})
	})
}
