# E2E Minimal LogicFlowRuntime Lifecycle Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update sample CRs to be realistic, self-contained examples and add e2e test cases that verify a minimal LogicFlowRuntime lifecycle on a real Kind cluster — create, reconcile to Ready, verify GC on delete.

**Architecture:** The e2e tests use the existing Kubebuilder scaffold (Ginkgo + Kind cluster). The operator image is built and loaded into Kind by `BeforeSuite`. Tests apply CRs via `kubectl apply`, then poll with `Eventually` for the Deployment, pod readiness, and Runtime status. The minimal runner image (`quay.io/quarkiverse/quarkus-flow-runner:0.15.1-minimal`) starts without config and passes Quarkus SmallRye health checks on port 8080 (`/q/health/live`, `/q/health/ready`). No persistence, leases, or ServiceAccount/RBAC are needed for the minimal case.

**Tech Stack:** Go, Ginkgo v2 + Gomega, Kind, kubectl (via `os/exec`), existing `test/utils` helpers

## Global Constraints

- Do NOT add license headers to new files (automated script handles this)
- Do NOT create git commits — stage changes only, user commits manually
- E2e tests run in `test/e2e/` package, use `kubectl` via `os/exec` (not client-go)
- Use existing helpers: `utils.Run(cmd)`, `utils.GetNonEmptyLines(output)`
- All resources are created in the operator namespace (`logic-operator-system`)
- Use `Eventually` with the default 2-minute timeout and 1-second polling interval already set in `e2e_test.go:137`
- Clean up all test resources in `AfterEach` / `AfterAll`, even on failure
- The quarkus-flow minimal runner image (`quay.io/quarkiverse/quarkus-flow-runner:0.15.1-minimal`) is pulled from the public registry — it is NOT built or loaded into Kind
- Sample CRs must be valid against the current CRD schema
- Sample `LogicFlowRuntime` name should match the `LogicFlowDefinition` runtimeRef so both samples work as a pair

---

### Task 1: Update sample CRs to realistic, self-contained examples

**Files:**
- Modify: `config/samples/logic_v1_logicflowruntime.yaml`
- Modify: `config/samples/logic_v1_logicflowdefinition.yaml`

**Interfaces:**
- Consumes: nothing
- Produces: A pair of valid sample CRs — a minimal Runtime and a hello-world Definition that can be applied together and executed locally without external dependencies

- [ ] **Step 1: Replace the LogicFlowRuntime stub with a minimal runtime**

Replace the contents of `config/samples/logic_v1_logicflowruntime.yaml` with:

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  labels:
    app.kubernetes.io/name: logic-operator
    app.kubernetes.io/managed-by: kustomize
  name: hello-runtime
spec: {}
```

This CR creates a runtime with operator defaults: 1 replica, `quay.io/quarkiverse/quarkus-flow-runner:0.15.1-minimal` image (auto-selected when no persistence), default probes, no persistence/leases.

- [ ] **Step 2: Replace the LogicFlowDefinition with a hello-world workflow**

Replace the contents of `config/samples/logic_v1_logicflowdefinition.yaml` with:

```yaml
apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowDefinition
metadata:
  labels:
    app.kubernetes.io/name: logic-operator
    app.kubernetes.io/managed-by: kustomize
  name: hello-world-v1-0-0
spec:
  runtimeRef:
    name: hello-runtime
  flow:
    document:
      dsl: '1.0.0'
      namespace: examples
      name: hello-world
      version: '1.0.0'
    do:
      - greet:
          set:
            message: '${ "Hello, " + .name + "!" }'
```

This is the canonical quarkus-flow hello-world workflow. It uses a `set` task with a jq expression — fully self-contained, no external HTTP calls. Accepts a `name` input and produces a `message` output.

- [ ] **Step 3: Verify both samples validate against the CRD**

Run:
```bash
make install
kubectl apply --dry-run=server -f config/samples/logic_v1_logicflowruntime.yaml
kubectl apply --dry-run=server -f config/samples/logic_v1_logicflowdefinition.yaml
```

Expected: no validation errors.

- [ ] **Step 4: Stage the changes**

```bash
git add config/samples/logic_v1_logicflowruntime.yaml config/samples/logic_v1_logicflowdefinition.yaml
```

---

### Task 2: Add e2e test for minimal LogicFlowRuntime lifecycle

**Files:**
- Modify: `test/e2e/e2e_test.go` (add new `Context` block after the existing `Manager` context)

**Interfaces:**
- Consumes: Operator already deployed and running from `BeforeAll` in the existing `Manager` context (the `Ordered` container ensures this)
- Produces: E2e coverage for: create → Deployment exists → pod Ready → Runtime status Ready → delete → GC

The tests go inside the existing `Describe("Manager", Ordered, ...)` block so they share the `BeforeAll` (deploy operator) and `AfterAll` (undeploy) lifecycle. They use `Ordered` test ordering to reuse the same CR across multiple `It` blocks — create once, verify progressively, delete at the end.

- [ ] **Step 1: Write the test constants and CR YAML**

Add after the existing `metricsRoleBindingName` constant block in `test/e2e/e2e_test.go`:

```go
const testRuntimeName = "e2e-minimal-rt"
const testRuntimeYAML = `apiVersion: logic.kubesmarts.org/v1
kind: LogicFlowRuntime
metadata:
  name: e2e-minimal-rt
  namespace: logic-operator-system
spec: {}
`
```

- [ ] **Step 2: Write the e2e test Context**

Add a new `Context` block after the existing `Context("Manager", ...)` block but still inside `Describe("Manager", Ordered, ...)`:

```go
Context("LogicFlowRuntime minimal lifecycle", Ordered, func() {
    BeforeAll(func() {
        By("applying a minimal LogicFlowRuntime CR")
        cmd := exec.Command("kubectl", "apply", "-f", "-")
        cmd.Stdin = strings.NewReader(testRuntimeYAML)
        _, err := utils.Run(cmd)
        Expect(err).NotTo(HaveOccurred(), "Failed to create LogicFlowRuntime")
    })

    AfterAll(func() {
        By("deleting the LogicFlowRuntime CR")
        cmd := exec.Command("kubectl", "delete", "logicflowruntime",
            testRuntimeName, "-n", namespace, "--ignore-not-found")
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

    It("should clean up child resources on deletion", func() {
        By("deleting the LogicFlowRuntime CR")
        cmd := exec.Command("kubectl", "delete", "logicflowruntime",
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
```

- [ ] **Step 3: Add the `strings` import**

The `strings` package is needed for `strings.NewReader`. Add it to the import block in `test/e2e/e2e_test.go` if not already present:

```go
import (
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/kubesmarts/logic-operator/test/utils"
)
```

- [ ] **Step 4: Verify the test compiles**

Run:
```bash
cd test/e2e && go vet ./...
```

Expected: no errors.

- [ ] **Step 5: Stage the changes**

```bash
git add test/e2e/e2e_test.go
```

---

## Notes

**Image pull:** The minimal runner image is pulled from the public `quay.io/quarkiverse` registry. In CI, the Kind cluster needs outbound network access to pull it. If the image pull is slow, increase the `Eventually` timeout for the Deployment readiness check (currently 3 minutes).

**Pod security:** The operator namespace has `pod-security.kubernetes.io/enforce=restricted`. The quarkus-flow runner image runs as non-root by default (Quarkus convention), so it should comply. If the pod gets rejected by the restricted policy, you may need to add `securityContext` fields to the Runtime CR or update the operator's `ToDeploymentSpec` to set a default security context.

**Test ordering:** The `Ordered` container ensures `It` blocks run in declaration order. The last test (`should clean up child resources on deletion`) deletes the CR, so `AfterAll` uses `--ignore-not-found` to handle the case where it was already deleted.
