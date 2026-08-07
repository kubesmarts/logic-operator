# LogicFlowDefinition Controller Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the LogicFlowDefinition controller that materializes OWS flow documents into labeled ConfigMaps for Runtime discovery.

**Architecture:** The controller follows the established SSA reconciler pattern from LogicFlowRuntime. On each reconcile it validates the runtimeRef, parses the flow document, applies a ConfigMap via SSA, and updates status with workflow metadata and conditions. The ConfigMap is owned by the Definition CR and labeled for Runtime discovery.

**Tech Stack:** Go, controller-runtime v0.24.1, Kubernetes apply configurations (SSA), Ginkgo v2 + Gomega (envtest), open-workflow-specification/sdk-go/v4

## Global Constraints

- Do NOT add license headers to new files (automated script handles this)
- Follow SSA pattern from `internal/controller/logicflowruntime_controller.go`
- Use `FieldOwnerLogicOperator` ("logic-operator") for all SSA apply calls
- Use `OwnerRef()` from `objects_common.go` for owner references
- Use `logicv1.SetCondition()` from `api/v1/status_types.go` for condition management
- Condition/reason constants already exist in `api/v1/status_types.go`
- The `LogicFlowDefinitionReconciler` struct and `SetupWithManager` (with `Owns(&ConfigMap{})`) already exist in the scaffold

---

### Task 1: Add LogicFlowDefinitionKind constant and ConfigMap label constants

**Files:**
- Modify: `api/v1/logicflowdefinition_types.go:17-27` (add kind constant after imports)
- Modify: `internal/controller/objects_common.go:14-18` (add label and prefix constants)

**Interfaces:**
- Consumes: nothing
- Produces:
  - `logicv1.LogicFlowDefinitionKind` — string constant `"LogicFlowDefinition"`, used by `OwnerRef()` calls in later tasks
  - `controller.ConfigMapPrefix` — string constant `"lfd-"`, used to name ConfigMaps
  - `controller.LabelRuntimeRef` — string `"logic.kubesmarts.org/runtime-ref"`, label key for Runtime discovery
  - `controller.LabelWorkflowName` — string `"logic.kubesmarts.org/workflow-name"`
  - `controller.LabelWorkflowVersion` — string `"logic.kubesmarts.org/workflow-version"`

- [ ] **Step 1: Add `LogicFlowDefinitionKind` constant**

In `api/v1/logicflowdefinition_types.go`, add after the imports block (before `LogicFlowDefinitionSpec`):

```go
const LogicFlowDefinitionKind = "LogicFlowDefinition"
```

This mirrors the existing `LogicFlowRuntimeKind` in `logicflowruntime_types.go:24`.

- [ ] **Step 2: Add ConfigMap label and prefix constants**

In `internal/controller/objects_common.go`, add to the existing constants block:

```go
const (
	ContainerNameRunner     = "logic-runner"
	FieldOwnerLogicOperator = "logic-operator"
	LabelManagedBy          = "logic-operator"
	LabelPartOf             = "logic-platform"

	ConfigMapPrefix      = "lfd-"
	LabelRuntimeRef      = "logic.kubesmarts.org/runtime-ref"
	LabelWorkflowName    = "logic.kubesmarts.org/workflow-name"
	LabelWorkflowVersion = "logic.kubesmarts.org/workflow-version"
)
```

- [ ] **Step 3: Verify build**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go build ./...`
Expected: Build succeeds with no errors.

- [ ] **Step 4: Run existing tests to verify no regressions**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./api/v1/... ./internal/controller/... -count=1`
Expected: All existing tests pass.

- [ ] **Step 5: Commit**

```bash
git add api/v1/logicflowdefinition_types.go internal/controller/objects_common.go
git commit -m "feat: add LogicFlowDefinitionKind and ConfigMap label constants"
```

---

### Task 2: Add RBAC marker for LogicFlowRuntime read access and ConfigMap write access

**Files:**
- Modify: `internal/controller/logicflowdefinition_controller.go:37-39` (add RBAC markers)

**Interfaces:**
- Consumes: nothing
- Produces: RBAC permissions for the Definition controller to GET LogicFlowRuntimes and manage ConfigMaps

- [ ] **Step 1: Add RBAC markers**

In `internal/controller/logicflowdefinition_controller.go`, add two RBAC markers after the existing three:

```go
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/finalizers,verbs=update
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
```

- [ ] **Step 2: Regenerate RBAC manifests**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && make manifests`
Expected: `config/rbac/role.yaml` is updated with the new permissions.

- [ ] **Step 3: Verify the generated role includes the new permissions**

Run: `grep -A5 "configmaps" config/rbac/role.yaml`
Expected: A rules entry for configmaps with the specified verbs.

- [ ] **Step 4: Commit**

```bash
git add internal/controller/logicflowdefinition_controller.go config/rbac/
git commit -m "feat: add RBAC markers for LogicFlowDefinition controller"
```

---

### Task 3: Implement ConfigMap builder function

**Files:**
- Create: `internal/controller/definition_configmap.go`
- Test: `internal/controller/definition_configmap_test.go`

**Interfaces:**
- Consumes:
  - `controller.ConfigMapPrefix` — from Task 1
  - `controller.LabelRuntimeRef`, `controller.LabelWorkflowName`, `controller.LabelWorkflowVersion` — from Task 1
  - `controller.LabelManagedBy` — existing constant
  - `controller.OwnerRef(owner metav1.Object, kind string) *metav1ac.OwnerReferenceApplyConfiguration` — existing function
  - `logicv1.LogicFlowDefinitionKind` — from Task 1
- Produces:
  - `controller.DefinitionConfigMap(def *logicv1.LogicFlowDefinition, wf *model.Workflow) *corev1ac.ConfigMapApplyConfiguration` — builds the SSA ConfigMap apply configuration

- [ ] **Step 1: Write the failing tests**

Create `internal/controller/definition_configmap_test.go`:

```go
package controller

import (
	"testing"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/open-workflow-specification/sdk-go/v4/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func testDefinition(name, namespace, runtimeRef string, flowJSON []byte) *logicv1.LogicFlowDefinition {
	return &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-uid-" + name),
		},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: runtimeRef},
			Flow:       runtime.RawExtension{Raw: flowJSON},
		},
	}
}

func TestDefinitionConfigMap_Name(t *testing.T) {
	def := testDefinition("payment-processor-v1", "default", "payments-runtime", nil)
	wf := &model.Workflow{
		Document: model.Document{
			Name:    "payment-processor",
			Version: "1.0.0",
		},
	}

	cm := DefinitionConfigMap(def, wf)

	require.NotNil(t, cm.Name)
	assert.Equal(t, "lfd-payment-processor-v1", *cm.Name)
	require.NotNil(t, cm.Namespace)
	assert.Equal(t, "default", *cm.Namespace)
}

func TestDefinitionConfigMap_Labels(t *testing.T) {
	def := testDefinition("my-flow-v2", "production", "my-runtime", nil)
	wf := &model.Workflow{
		Document: model.Document{
			Name:      "my-flow",
			Version:   "2.0.0",
			Namespace: "orders",
		},
	}

	cm := DefinitionConfigMap(def, wf)

	assert.Equal(t, "my-runtime", cm.Labels[LabelRuntimeRef])
	assert.Equal(t, LabelManagedBy, cm.Labels["app.kubernetes.io/managed-by"])
	assert.Equal(t, "my-flow", cm.Labels[LabelWorkflowName])
	assert.Equal(t, "2.0.0", cm.Labels[LabelWorkflowVersion])
}

func TestDefinitionConfigMap_OwnerReference(t *testing.T) {
	def := testDefinition("test-def", "default", "test-rt", nil)
	wf := &model.Workflow{Document: model.Document{Name: "test", Version: "1.0.0"}}

	cm := DefinitionConfigMap(def, wf)

	require.Len(t, cm.OwnerReferences, 1)
	ownerRef := cm.OwnerReferences[0]
	assert.Equal(t, logicv1.LogicFlowDefinitionKind, *ownerRef.Kind)
	assert.Equal(t, "test-def", *ownerRef.Name)
	assert.Equal(t, types.UID("test-uid-test-def"), *ownerRef.UID)
	assert.True(t, *ownerRef.Controller)
	assert.True(t, *ownerRef.BlockOwnerDeletion)
}

func TestDefinitionConfigMap_DataKey(t *testing.T) {
	flowJSON := []byte(`{"document":{"dsl":"1.0.0","name":"my-wf","version":"1.0.0","namespace":"ns"},"do":[]}`)
	def := testDefinition("my-wf-v1", "default", "rt", flowJSON)
	wf := &model.Workflow{Document: model.Document{Name: "my-wf", Version: "1.0.0"}}

	cm := DefinitionConfigMap(def, wf)

	require.Contains(t, cm.Data, "my-wf.json")
	assert.Equal(t, string(flowJSON), cm.Data["my-wf.json"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -run TestDefinitionConfigMap -count=1 -v`
Expected: Compilation error — `DefinitionConfigMap` is undefined.

- [ ] **Step 3: Implement DefinitionConfigMap**

Create `internal/controller/definition_configmap.go`:

```go
package controller

import (
	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	"github.com/open-workflow-specification/sdk-go/v4/model"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

func DefinitionConfigMap(def *logicv1.LogicFlowDefinition, wf *model.Workflow) *corev1ac.ConfigMapApplyConfiguration {
	cmName := ConfigMapPrefix + def.Name
	dataKey := wf.Document.Name + ".json"

	labels := map[string]string{
		LabelRuntimeRef:              def.Spec.RuntimeRef.Name,
		"app.kubernetes.io/managed-by": LabelManagedBy,
		LabelWorkflowName:            wf.Document.Name,
		LabelWorkflowVersion:         wf.Document.Version,
	}

	return corev1ac.ConfigMap(cmName, def.Namespace).
		WithLabels(labels).
		WithOwnerReferences(OwnerRef(def, logicv1.LogicFlowDefinitionKind)).
		WithData(map[string]string{
			dataKey: string(def.Spec.Flow.Raw),
		})
}
```

- [ ] **Step 4: Add missing import to test file**

The test file needs `corev1 "k8s.io/api/core/v1"` in its imports for `testDefinition`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -run TestDefinitionConfigMap -count=1 -v`
Expected: All 4 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/definition_configmap.go internal/controller/definition_configmap_test.go
git commit -m "feat: add DefinitionConfigMap builder for flow-to-ConfigMap materialization"
```

---

### Task 4: Implement the LogicFlowDefinition Reconcile method

**Files:**
- Modify: `internal/controller/logicflowdefinition_controller.go` (replace scaffold with full implementation)

**Interfaces:**
- Consumes:
  - `controller.DefinitionConfigMap(def, wf)` — from Task 3
  - `controller.FieldOwnerLogicOperator` — existing constant
  - `logicv1.SetCondition(conditions, type, status, gen, reason, message)` — existing function
  - `logicv1.ConditionRuntimeRefValid`, `logicv1.ConditionFlowParsed`, `logicv1.ConditionConfigMapReady` — existing constants
  - `logicv1.ReasonRuntimeNotFound`, `logicv1.ReasonParseError`, `logicv1.ReasonSSAApplyFailed`, `logicv1.ReasonReady` — existing constants
  - `logicv1.LogicFlowDefinitionKind` — from Task 1
  - `ConfigMapPrefix` — from Task 1
- Produces:
  - `LogicFlowDefinitionReconciler.Reconcile(ctx, req) (ctrl.Result, error)` — full reconciliation logic

- [ ] **Step 1: Implement the Reconcile method**

Replace the entire content of `internal/controller/logicflowdefinition_controller.go` with:

```go
package controller

import (
	"context"
	"fmt"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type LogicFlowDefinitionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowdefinitions/finalizers,verbs=update
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *LogicFlowDefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var def logicv1.LogicFlowDefinition
	if err := r.Get(ctx, req.NamespacedName, &def); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate runtimeRef exists
	var rt logicv1.LogicFlowRuntime
	rtKey := client.ObjectKey{Name: def.Spec.RuntimeRef.Name, Namespace: def.Namespace}
	if err := r.Get(ctx, rtKey, &rt); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("referenced LogicFlowRuntime not found", "runtimeRef", def.Spec.RuntimeRef.Name)
			logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRefValid, metav1.ConditionFalse, def.Generation, logicv1.ReasonRuntimeNotFound, fmt.Sprintf("LogicFlowRuntime %q not found", def.Spec.RuntimeRef.Name))
			return ctrl.Result{}, r.Status().Update(ctx, &def)
		}
		return ctrl.Result{}, err
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionRuntimeRefValid, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	// Parse flow document
	wf, err := def.Spec.ParseFlow()
	if err != nil {
		log.Error(err, "failed to parse flow document")
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionFlowParsed, metav1.ConditionFalse, def.Generation, logicv1.ReasonParseError, err.Error())
		return ctrl.Result{}, r.Status().Update(ctx, &def)
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionFlowParsed, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	// Apply ConfigMap via SSA
	cm := DefinitionConfigMap(&def, wf)
	if err := r.Apply(ctx, cm, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership); err != nil {
		log.Error(err, "failed to apply ConfigMap")
		logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionConfigMapReady, metav1.ConditionFalse, def.Generation, logicv1.ReasonSSAApplyFailed, err.Error())
		return ctrl.Result{}, r.Status().Update(ctx, &def)
	}
	logicv1.SetCondition(&def.Status.Conditions, logicv1.ConditionConfigMapReady, metav1.ConditionTrue, def.Generation, logicv1.ReasonReady, "")

	// Update status
	def.Status.ObservedGeneration = def.Generation
	def.Status.WorkflowName = wf.Document.Name
	def.Status.WorkflowVersion = wf.Document.Version
	def.Status.WorkflowNamespace = wf.Document.Namespace
	def.Status.ConfigMapRef = &corev1.LocalObjectReference{Name: ConfigMapPrefix + def.Name}

	return ctrl.Result{}, r.Status().Update(ctx, &def)
}

func (r *LogicFlowDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowDefinition{}).
		Owns(&corev1.ConfigMap{}).
		Named("logicflowdefinition").
		Complete(r)
}
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go build ./...`
Expected: Build succeeds.

- [ ] **Step 3: Run existing tests**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/... -count=1`
Expected: All existing tests pass (the scaffold test still works since the controller now does more, not less).

- [ ] **Step 4: Commit**

```bash
git add internal/controller/logicflowdefinition_controller.go
git commit -m "feat: implement LogicFlowDefinition reconciler with runtimeRef validation, flow parsing, and ConfigMap SSA"
```

---

### Task 5: Write comprehensive envtest tests for the controller

**Files:**
- Modify: `internal/controller/logicflowdefinition_controller_test.go` (replace scaffold tests with full test suite)

**Interfaces:**
- Consumes:
  - `controller.LogicFlowDefinitionReconciler` — from Task 4
  - `controller.ConfigMapPrefix` — from Task 1
  - `controller.LabelRuntimeRef`, `controller.LabelWorkflowName`, `controller.LabelWorkflowVersion` — from Task 1
  - `logicv1.LogicFlowDefinitionKind` — from Task 1
  - `logicv1.ConditionRuntimeRefValid`, `logicv1.ConditionFlowParsed`, `logicv1.ConditionConfigMapReady` — existing constants
  - `logicv1.ReasonRuntimeNotFound`, `logicv1.ReasonParseError` — existing constants
  - Package-level `ctx`, `k8sClient` from `suite_test.go`
- Produces: Complete test coverage for the Definition controller

- [ ] **Step 1: Replace the scaffold test file**

Replace the entire content of `internal/controller/logicflowdefinition_controller_test.go` with:

```go
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newDefReconciler() *LogicFlowDefinitionReconciler {
	return &LogicFlowDefinitionReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

func reconcileDefAndFetch(ctx context.Context, r *LogicFlowDefinitionReconciler, nn types.NamespacedName) *logicv1.LogicFlowDefinition {
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
	Expect(err).NotTo(HaveOccurred())
	var def logicv1.LogicFlowDefinition
	Expect(k8sClient.Get(ctx, nn, &def)).To(Succeed())
	return &def
}

func validFlowJSON(name, version, namespace string) []byte {
	return []byte(`{"document":{"dsl":"1.0.0","namespace":"` + namespace + `","name":"` + name + `","version":"` + version + `"},"do":[{"noop":{"call":"http","with":{"method":"get","endpoint":"http://example.com"}}}]}`)
}

func createDefinition(ctx context.Context, name, runtimeRef string, flowJSON []byte) types.NamespacedName {
	nn := types.NamespacedName{Name: name, Namespace: "default"}
	def := &logicv1.LogicFlowDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: logicv1.LogicFlowDefinitionSpec{
			RuntimeRef: corev1.LocalObjectReference{Name: runtimeRef},
			Flow:       runtime.RawExtension{Raw: flowJSON},
		},
	}
	Expect(k8sClient.Create(ctx, def)).To(Succeed())
	return nn
}

func deleteDefinition(ctx context.Context, nn types.NamespacedName) {
	def := &logicv1.LogicFlowDefinition{}
	err := k8sClient.Get(ctx, nn, def)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, def)).To(Succeed())
}

func createRuntimeForDef(ctx context.Context, name string) types.NamespacedName {
	nn := types.NamespacedName{Name: name, Namespace: "default"}
	rt := &logicv1.LogicFlowRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       logicv1.LogicFlowRuntimeSpec{},
	}
	Expect(k8sClient.Create(ctx, rt)).To(Succeed())
	return nn
}

func deleteRuntime2(ctx context.Context, nn types.NamespacedName) {
	rt := &logicv1.LogicFlowRuntime{}
	err := k8sClient.Get(ctx, nn, rt)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, rt)).To(Succeed())
}

var _ = Describe("LogicFlowDefinition Controller", func() {

	Context("Valid flow with existing Runtime", func() {
		const defName = "test-def-valid"
		const rtName = "test-def-rt"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("payment-processor", "1.0.0", "payments"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should create a ConfigMap with correct name and namespace", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())
			Expect(cm.Name).To(Equal("lfd-" + defName))
		})

		It("should set correct labels on the ConfigMap", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())

			Expect(cm.Labels).To(HaveKeyWithValue(LabelRuntimeRef, rtName))
			Expect(cm.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", LabelManagedBy))
			Expect(cm.Labels).To(HaveKeyWithValue(LabelWorkflowName, "payment-processor"))
			Expect(cm.Labels).To(HaveKeyWithValue(LabelWorkflowVersion, "1.0.0"))
		})

		It("should set owner references on the ConfigMap", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())

			Expect(cm.OwnerReferences).To(HaveLen(1))
			Expect(cm.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowDefinitionKind))
			Expect(cm.OwnerReferences[0].Name).To(Equal(defName))
			Expect(cm.OwnerReferences[0].UID).To(Equal(def.UID))
			Expect(*cm.OwnerReferences[0].Controller).To(BeTrue())
			Expect(*cm.OwnerReferences[0].BlockOwnerDeletion).To(BeTrue())
		})

		It("should set the flow data under the workflow name key", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())

			Expect(cm.Data).To(HaveKey("payment-processor.json"))
			Expect(cm.Data["payment-processor.json"]).To(ContainSubstring(`"name":"payment-processor"`))
		})

		It("should set status fields on first reconcile", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			Expect(def.Status.ObservedGeneration).To(Equal(def.Generation))
			Expect(def.Status.WorkflowName).To(Equal("payment-processor"))
			Expect(def.Status.WorkflowVersion).To(Equal("1.0.0"))
			Expect(def.Status.WorkflowNamespace).To(Equal("payments"))
			Expect(def.Status.ConfigMapRef).NotTo(BeNil())
			Expect(def.Status.ConfigMapRef.Name).To(Equal(ConfigMapPrefix + defName))
		})

		It("should set all conditions to True", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			rtCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionTrue))

			flowCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionFlowParsed)
			Expect(flowCond).NotTo(BeNil())
			Expect(flowCond.Status).To(Equal(metav1.ConditionTrue))

			cmCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionConfigMapReady)
			Expect(cmCond).NotTo(BeNil())
			Expect(cmCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("RuntimeRef not found", func() {
		const defName = "test-def-no-rt"
		var defNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			defNN = createDefinition(ctx, defName, "nonexistent-runtime", validFlowJSON("wf", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
		})

		It("should not create a ConfigMap", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			err := k8sClient.Get(ctx, cmNN, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should set RuntimeRefValid condition to False", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			rtCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(rtCond.Reason).To(Equal(logicv1.ReasonRuntimeNotFound))
		})
	})

	Context("Invalid flow JSON", func() {
		const defName = "test-def-bad-flow"
		const rtName = "test-def-rt-bad"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, []byte(`{not valid json`))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should not create a ConfigMap", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			err := k8sClient.Get(ctx, cmNN, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should set FlowParsed condition to False", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			flowCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionFlowParsed)
			Expect(flowCond).NotTo(BeNil())
			Expect(flowCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(flowCond.Reason).To(Equal(logicv1.ReasonParseError))
		})

		It("should set RuntimeRefValid to True (runtime exists)", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)

			rtCond := meta.FindStatusCondition(def.Status.Conditions, logicv1.ConditionRuntimeRefValid)
			Expect(rtCond).NotTo(BeNil())
			Expect(rtCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Spec update via SSA", func() {
		const defName = "test-def-ssa"
		const rtName = "test-def-rt-ssa"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("wf-v1", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should update the ConfigMap when flow changes", func() {
			reconcileDefAndFetch(ctx, r, defNN)

			var def logicv1.LogicFlowDefinition
			Expect(k8sClient.Get(ctx, defNN, &def)).To(Succeed())
			def.Spec.Flow = runtime.RawExtension{Raw: validFlowJSON("wf-v2", "2.0.0", "ns2")}
			Expect(k8sClient.Update(ctx, &def)).To(Succeed())

			updated := reconcileDefAndFetch(ctx, r, defNN)

			Expect(updated.Status.WorkflowName).To(Equal("wf-v2"))
			Expect(updated.Status.WorkflowVersion).To(Equal("2.0.0"))
			Expect(updated.Status.WorkflowNamespace).To(Equal("ns2"))

			var cm corev1.ConfigMap
			cmNN := types.NamespacedName{Name: ConfigMapPrefix + defName, Namespace: "default"}
			Expect(k8sClient.Get(ctx, cmNN, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("wf-v2.json"))
			Expect(cm.Labels[LabelWorkflowName]).To(Equal("wf-v2"))
			Expect(cm.Labels[LabelWorkflowVersion]).To(Equal("2.0.0"))
		})

		It("should update ObservedGeneration after spec change", func() {
			def := reconcileDefAndFetch(ctx, r, defNN)
			gen1 := def.Status.ObservedGeneration

			Expect(k8sClient.Get(ctx, defNN, def)).To(Succeed())
			def.Spec.Flow = runtime.RawExtension{Raw: validFlowJSON("wf-v3", "3.0.0", "ns")}
			Expect(k8sClient.Update(ctx, def)).To(Succeed())

			def = reconcileDefAndFetch(ctx, r, defNN)
			Expect(def.Status.ObservedGeneration).To(BeNumerically(">", gen1))
			Expect(def.Status.ObservedGeneration).To(Equal(def.Generation))
		})
	})

	Context("Idempotency", func() {
		const defName = "test-def-idempotent"
		const rtName = "test-def-rt-idem"
		var defNN, rtNN types.NamespacedName
		var r *LogicFlowDefinitionReconciler

		BeforeEach(func() {
			r = newDefReconciler()
			rtNN = createRuntimeForDef(ctx, rtName)
			defNN = createDefinition(ctx, defName, rtName, validFlowJSON("wf", "1.0.0", "ns"))
		})
		AfterEach(func() {
			deleteDefinition(ctx, defNN)
			deleteRuntime2(ctx, rtNN)
		})

		It("should produce the same result on repeated reconciliation", func() {
			def1 := reconcileDefAndFetch(ctx, r, defNN)
			gen1 := def1.Status.ObservedGeneration
			condCount1 := len(def1.Status.Conditions)

			def2 := reconcileDefAndFetch(ctx, r, defNN)
			Expect(def2.Status.ObservedGeneration).To(Equal(gen1))
			Expect(def2.Status.Conditions).To(HaveLen(condCount1))
			Expect(def2.Status.WorkflowName).To(Equal(def1.Status.WorkflowName))
		})
	})

	Context("CR not found", func() {
		It("should return success for a missing CR", func() {
			r := newDefReconciler()
			nn := types.NamespacedName{Name: "does-not-exist-def", Namespace: "default"}

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			var cm corev1.ConfigMap
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ConfigMapPrefix + "does-not-exist-def", Namespace: "default"}, &cm)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
```

- [ ] **Step 2: Run the full test suite**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/... -count=1 -v`
Expected: All tests pass — both new LogicFlowDefinition tests and existing LogicFlowRuntime tests.

- [ ] **Step 3: Run with race detector**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/... -race -count=1`
Expected: No race conditions detected.

- [ ] **Step 4: Commit**

```bash
git add internal/controller/logicflowdefinition_controller_test.go
git commit -m "test: add comprehensive envtest tests for LogicFlowDefinition controller"
```

---

### Task 6: Update sample CR and verify end-to-end build

**Files:**
- Modify: `config/samples/logic_v1_logicflowdefinition.yaml` (already has good content, verify it works)

**Interfaces:**
- Consumes: All previous tasks
- Produces: Verified build and manifests

- [ ] **Step 1: Regenerate all manifests**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && make manifests generate fmt vet`
Expected: All generated files are up to date, no errors.

- [ ] **Step 2: Run full test suite including API tests**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./... -count=1 | grep -v /e2e`
Expected: All tests pass.

- [ ] **Step 3: Run linter**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && make lint`
Expected: No lint errors.

- [ ] **Step 4: Commit any generated file changes**

```bash
git add config/ api/ internal/
git commit -m "chore: regenerate manifests after LogicFlowDefinition controller implementation"
```

---

## Post-Implementation: Runtime Controller ConfigMap Integration

> **Not part of this plan.** This is documented here as the follow-up task that connects the Definition controller output to the Runtime controller input.

The LogicFlowRuntime controller needs these additions:

1. **New watch:** `Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(mapConfigMapToRuntime))` — the mapper reads the `logic.kubesmarts.org/runtime-ref` label and enqueues the corresponding Runtime.
2. **Volume mounting:** On reconcile, list ConfigMaps with label `runtime-ref=<runtime-name>`, add a `Volume` (ConfigMap source) and `VolumeMount` per ConfigMap to the Deployment apply configuration.
3. **Status update:** Populate `status.definitions[]` (name, service path, version) and `status.configMapRefs[]` from the discovered ConfigMaps.

This should be a separate implementation plan once the Definition controller is merged.
