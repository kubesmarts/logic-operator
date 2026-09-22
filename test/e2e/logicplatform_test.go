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

const platformName = "e2e-platform"

// platformYAML - LogicPlatform with DataIndex enabled
// Uses the same PostgreSQL infra as durable tests
const platformYAML = `
apiVersion: logic.kubesmarts.org/v1
kind: LogicPlatform
metadata:
  name: e2e-platform
  namespace: logic-operator-system
spec:
  dataIndex:
    enabled: true
    persistence:
      postgresql:
        secretRef:
          name: platform-pg-secret
        serviceRef:
          name: postgresql
          namespace: e2e-durable-infra
          databaseSchema: dataindex
`

// platformPGSecretYAML creates credentials secret for DataIndex
const platformPGSecretYAML = `
apiVersion: v1
kind: Secret
metadata:
  name: platform-pg-secret
  namespace: logic-operator-system
type: Opaque
stringData:
  POSTGRESQL_USER: flowuser
  POSTGRESQL_PASSWORD: flowpass
`

func platformTests() {
	Context("LogicPlatform with DataIndex", Ordered, func() {
		BeforeAll(func() {
			By("creating infra namespace for PostgreSQL (if not exists)")
			cmd := exec.Command("kubectl", "create", "namespace", durableInfraNamespace)
			_, _ = utils.Run(cmd) // ignore error if namespace already exists

			By("creating PostgreSQL credentials secret for platform")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(platformPGSecretYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("deploying PostgreSQL infra (if not exists)")
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

			By("creating the LogicPlatform")
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(platformYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			By("deleting the LogicPlatform")
			cmd := exec.Command("kubectl", "delete", "logicplatform", platformName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting the platform secret")
			cmd = exec.Command("kubectl", "delete", "secret", "platform-pg-secret",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			// Note: PostgreSQL infra namespace is cleaned up in the global AfterAll
		})

		It("should deploy DataIndex deployment", func() {
			By("waiting for DataIndex deployment to be created")
			waitForDeployment := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", platformName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(platformName))
			}
			Eventually(waitForDeployment, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying deployment has correct image")
			cmd := exec.Command("kubectl", "get", "deployment", platformName,
				"-n", namespace,
				"-o", "jsonpath={.spec.template.spec.containers[0].image}")
			out, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("data-index-service"))
			Expect(out).To(ContainSubstring("postgresql"))

			By("verifying deployment has default resources")
			cmd = exec.Command("kubectl", "get", "deployment", platformName,
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
				cmd := exec.Command("kubectl", "get", "service", platformName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.name}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal(platformName))
			}
			Eventually(waitForService, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying service has correct ports")
			cmd := exec.Command("kubectl", "get", "service", platformName,
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
					"-l", "app.kubernetes.io/name="+platformName,
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
			cmd = exec.Command("kubectl", "get", "logicplatform", platformName,
				"-n", namespace,
				"-o", "jsonpath={.status.phase}")
			out, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("Ready"))
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
	})
}
