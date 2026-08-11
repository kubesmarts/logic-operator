package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func webhookValidationTests() {

	Context("LogicFlowDefinition webhook", func() {

		It("should reject a definition with an invalid flow document", func() {
			invalidDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: invalid-flow-def
  namespace: %s
spec:
  runtimeRef:
    name: some-runtime
  flow:
    not: "a valid workflow"`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(invalidDef, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for invalid flow")
			Expect(string(output)).To(ContainSubstring("spec.flow"))
		})

		It("should reject a definition with empty runtimeRef.name", func() {
			emptyRefDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: empty-ref-def
  namespace: %s
spec:
  runtimeRef:
    name: ""
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: test
      version: "1.0.0"
    do:
      - greet:
          set:
            message: hello`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(emptyRefDef, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for empty runtimeRef.name")
			Expect(string(output)).To(ContainSubstring("runtimeRef.name"))
		})

		It("should reject runtimeRef change on an existing definition", func() {
			// Create a valid definition first
			validDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: immutable-ref-def
  namespace: %s
spec:
  runtimeRef:
    name: some-runtime
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: immutable-test
      version: "1.0.0"
    do:
      - greet:
          set:
            message: hello`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(validDef, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "expected valid definition to be created: %s", string(output))

			// Try to change runtimeRef
			updatedDef := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  name: immutable-ref-def
  namespace: %s
spec:
  runtimeRef:
    name: other-runtime
  flow:
    document:
      dsl: "1.0.0"
      namespace: examples
      name: immutable-test
      version: "1.0.0"
    do:
      - greet:
          set:
            message: hello`

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(updatedDef, namespace))
			output, err = cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected runtimeRef change to be rejected")
			Expect(string(output)).To(ContainSubstring("runtimeRef"))

			// Cleanup
			cmd = exec.Command("kubectl", "delete", "logicflowdefinition",
				"immutable-ref-def", "-n", namespace, "--ignore-not-found")
			_ = cmd.Run()
		})
	})

	Context("LogicFlowRuntime webhook", func() {

		It("should reject API_KEY security type without apiKey spec", func() {
			invalidRT := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: invalid-security-rt
  namespace: %s
spec:
  security:
    type: API_KEY`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(invalidRT, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for API_KEY without keys")
			Expect(string(output)).To(ContainSubstring("apiKey"))
		})

		It("should reject OIDC security type without oidc spec", func() {
			invalidRT := `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: invalid-oidc-rt
  namespace: %s
spec:
  security:
    type: OIDC`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(invalidRT, namespace))
			output, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), "expected kubectl apply to fail for OIDC without config")
			Expect(string(output)).To(ContainSubstring("oidc"))
		})
	})
}
