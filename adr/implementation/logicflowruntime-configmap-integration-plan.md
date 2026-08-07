# LogicFlowRuntime ConfigMap Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate ConfigMap consumption into the LogicFlowRuntime controller so it discovers workflow ConfigMaps, mounts them as volumes on the Deployment, sets the `QUARKUS_FLOW_RUNNER_SOURCE_PATH` env var, and reflects definitions in status.

**Architecture:** The Definition controller already creates labeled ConfigMaps. The Runtime controller gains a `Watches` call that maps ConfigMaps with the `runtime-ref` label back to their owning Runtime. On each reconcile, it lists matching ConfigMaps, builds sorted volumes/volume mounts, injects them into the Deployment via SSA, and populates `status.definitions[]` and `status.configMapRefs[]`. The `QUARKUS_FLOW_RUNNER_SOURCE_PATH` env var is always set by the operator and cannot be overridden by users.

**Tech Stack:** Go, controller-runtime v0.24.1, Kubernetes apply configurations (SSA), Ginkgo v2 + Gomega (envtest), standard `testing` + `gomega` for unit tests

## Global Constraints

- Do NOT add license headers to new files (automated script handles this)
- Follow SSA pattern from `internal/controller/logicflowruntime_controller.go`
- Use `FieldOwnerLogicOperator` ("logic-operator") for all SSA apply calls
- Use existing `ContainerOption` pattern from `internal/controller/quarkus_config.go`
- ConfigMaps are sorted by name before building volumes to ensure deterministic Deployment specs
- `QUARKUS_FLOW_RUNNER_SOURCE_PATH` is immutable -- the operator always sets it to `/deployments/workflows`
- Unit tests for builder functions use standard `testing` + `gomega.NewWithT(t)` (matching `quarkus_config_test.go`)
- Controller envtest tests use Ginkgo v2 + Gomega (matching `logicflowruntime_controller_test.go`)
- The Runtime controller already has `configmaps` read RBAC -- no new markers needed

---

### Task 1: Volume builder functions, source path ContainerOption, and constant

**Files:**
- Modify: `internal/controller/quarkus_constants.go:3-12` (add `WorkflowMountPath` constant)
- Modify: `internal/controller/quarkus_config.go` (add `WithFlowSourcePath` and `WithFlowVolumeMounts` ContainerOptions)
- Modify: `internal/controller/objects_common.go` (add `FlowVolumes` function)
- Test: `internal/controller/quarkus_config_test.go` (unit tests for all three functions)

**Interfaces:**
- Consumes:
  - `ContainerOption` type from `objects_common.go:55` — `func(*corev1ac.ContainerApplyConfiguration)`
  - `envLiteral(name, value string)` from `quarkus_config.go:266` — builds literal env var apply configs
  - `WorkflowMountPath` constant (added in this task)
  - `corev1.ConfigMap` from `k8s.io/api/core/v1`
- Produces:
  - `controller.WorkflowMountPath` — string constant `"/deployments/workflows"`, used by `WithFlowVolumeMounts` and future tests
  - `controller.WithFlowSourcePath() ContainerOption` — sets `QUARKUS_FLOW_RUNNER_SOURCE_PATH` env var, filters out user-provided duplicates
  - `controller.WithFlowVolumeMounts(configMaps []corev1.ConfigMap) ContainerOption` — adds a read-only VolumeMount per ConfigMap
  - `controller.FlowVolumes(configMaps []corev1.ConfigMap) []*corev1ac.VolumeApplyConfiguration` — returns pod-level Volume entries for ConfigMaps

- [ ] **Step 1: Write unit tests for `WithFlowSourcePath`**

In `internal/controller/quarkus_config_test.go`, add at the end of the file:

```go
func TestWithFlowSourcePath_SetsEnvVar(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")

	WithFlowSourcePath()(c)

	g.Expect(c.Env).To(gomega.HaveLen(1))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("QUARKUS_FLOW_RUNNER_SOURCE_PATH"))
	g.Expect(*c.Env[0].Value).To(gomega.Equal(WorkflowMountPath))
}

func TestWithFlowSourcePath_OverridesUserValue(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test").
		WithEnv(corev1ac.EnvVar().WithName("QUARKUS_FLOW_RUNNER_SOURCE_PATH").WithValue("/custom/path"))

	WithFlowSourcePath()(c)

	var count int
	for _, e := range c.Env {
		if e.Name != nil && *e.Name == "QUARKUS_FLOW_RUNNER_SOURCE_PATH" {
			count++
			g.Expect(*e.Value).To(gomega.Equal(WorkflowMountPath))
		}
	}
	g.Expect(count).To(gomega.Equal(1), "expected exactly one QUARKUS_FLOW_RUNNER_SOURCE_PATH env var")
}

func TestWithFlowSourcePath_PreservesOtherEnvVars(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test").
		WithEnv(
			corev1ac.EnvVar().WithName("OTHER_VAR").WithValue("keep"),
			corev1ac.EnvVar().WithName("QUARKUS_FLOW_RUNNER_SOURCE_PATH").WithValue("/bad"),
			corev1ac.EnvVar().WithName("ANOTHER_VAR").WithValue("also-keep"),
		)

	WithFlowSourcePath()(c)

	g.Expect(c.Env).To(gomega.HaveLen(3))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("OTHER_VAR"))
	g.Expect(*c.Env[1].Name).To(gomega.Equal("ANOTHER_VAR"))
	g.Expect(*c.Env[2].Name).To(gomega.Equal("QUARKUS_FLOW_RUNNER_SOURCE_PATH"))
	g.Expect(*c.Env[2].Value).To(gomega.Equal(WorkflowMountPath))
}
```

- [ ] **Step 2: Write unit tests for `WithFlowVolumeMounts`**

In `internal/controller/quarkus_config_test.go`, add after the source path tests. Note: add `corev1 "k8s.io/api/core/v1"` and `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` to the imports.

```go
func testConfigMaps() []corev1.ConfigMap {
	return []corev1.ConfigMap{
		{ObjectMeta: metav1.ObjectMeta{Name: "lfd-order-flow"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "lfd-payment-processor"}},
	}
}

func TestWithFlowVolumeMounts_AddsOneMountPerConfigMap(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")
	cms := testConfigMaps()

	WithFlowVolumeMounts(cms)(c)

	g.Expect(c.VolumeMounts).To(gomega.HaveLen(2))
	g.Expect(*c.VolumeMounts[0].Name).To(gomega.Equal("lfd-order-flow"))
	g.Expect(*c.VolumeMounts[0].MountPath).To(gomega.Equal(WorkflowMountPath + "/lfd-order-flow"))
	g.Expect(*c.VolumeMounts[0].ReadOnly).To(gomega.BeTrue())
	g.Expect(*c.VolumeMounts[1].Name).To(gomega.Equal("lfd-payment-processor"))
	g.Expect(*c.VolumeMounts[1].MountPath).To(gomega.Equal(WorkflowMountPath + "/lfd-payment-processor"))
	g.Expect(*c.VolumeMounts[1].ReadOnly).To(gomega.BeTrue())
}

func TestWithFlowVolumeMounts_EmptyConfigMapsNoMounts(t *testing.T) {
	g := gomega.NewWithT(t)
	c := corev1ac.Container().WithName("test")

	WithFlowVolumeMounts(nil)(c)

	g.Expect(c.VolumeMounts).To(gomega.BeEmpty())
}
```

- [ ] **Step 3: Write unit tests for `FlowVolumes`**

In `internal/controller/quarkus_config_test.go`, add after the volume mount tests:

```go
func TestFlowVolumes_ReturnsOneVolumePerConfigMap(t *testing.T) {
	g := gomega.NewWithT(t)
	cms := testConfigMaps()

	vols := FlowVolumes(cms)

	g.Expect(vols).To(gomega.HaveLen(2))
	g.Expect(*vols[0].Name).To(gomega.Equal("lfd-order-flow"))
	g.Expect(*vols[0].ConfigMap.Name).To(gomega.Equal("lfd-order-flow"))
	g.Expect(*vols[1].Name).To(gomega.Equal("lfd-payment-processor"))
	g.Expect(*vols[1].ConfigMap.Name).To(gomega.Equal("lfd-payment-processor"))
}

func TestFlowVolumes_EmptyConfigMapsReturnsEmpty(t *testing.T) {
	g := gomega.NewWithT(t)

	vols := FlowVolumes(nil)

	g.Expect(vols).To(gomega.BeEmpty())
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -run "TestWithFlowSourcePath|TestWithFlowVolumeMounts|TestFlowVolumes" -v -count=1`
Expected: FAIL — `WithFlowSourcePath`, `WithFlowVolumeMounts`, `FlowVolumes`, `WorkflowMountPath` are undefined

- [ ] **Step 5: Add `WorkflowMountPath` constant**

In `internal/controller/quarkus_constants.go`, add to the existing const block:

```go
const (
	QuarkusFlowRegistry = "quay.io/quarkiverse"
	QuarkusFlowRunner   = "quarkus-flow-runner"
	QuarkusFlowVersion  = "0.15.1"

	ImageVariantMinimal  = "minimal"
	ImageVariantStandard = "standard"

	QuarkusPort = int32(8080)

	WorkflowMountPath = "/deployments/workflows"
)
```

- [ ] **Step 6: Implement `WithFlowSourcePath`**

In `internal/controller/quarkus_config.go`, add after the `WithSecurityEnvVars` function (line 173):

```go
func WithFlowSourcePath() ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		filtered := make([]*corev1ac.EnvVarApplyConfiguration, 0, len(c.Env))
		for _, e := range c.Env {
			if e.Name != nil && *e.Name == "QUARKUS_FLOW_RUNNER_SOURCE_PATH" {
				continue
			}
			filtered = append(filtered, e)
		}
		c.Env = filtered
		c.WithEnv(envLiteral("QUARKUS_FLOW_RUNNER_SOURCE_PATH", WorkflowMountPath))
	}
}
```

- [ ] **Step 7: Implement `WithFlowVolumeMounts`**

In `internal/controller/quarkus_config.go`, add after `WithFlowSourcePath`:

```go
func WithFlowVolumeMounts(configMaps []corev1.ConfigMap) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		for i := range configMaps {
			c.WithVolumeMounts(corev1ac.VolumeMount().
				WithName(configMaps[i].Name).
				WithMountPath(WorkflowMountPath + "/" + configMaps[i].Name).
				WithReadOnly(true))
		}
	}
}
```

Add `corev1 "k8s.io/api/core/v1"` to the imports of `quarkus_config.go` (it is not already imported -- the file uses `corev1ac` but not `corev1` for the `ConfigMap` type).

Wait -- `corev1` is already imported in `quarkus_config.go` (line 8: `corev1 "k8s.io/api/core/v1"`). No new import needed.

- [ ] **Step 8: Implement `FlowVolumes`**

In `internal/controller/objects_common.go`, add after the `MergeMaps` function (line 287):

```go
func FlowVolumes(configMaps []corev1.ConfigMap) []*corev1ac.VolumeApplyConfiguration {
	vols := make([]*corev1ac.VolumeApplyConfiguration, 0, len(configMaps))
	for i := range configMaps {
		vols = append(vols, corev1ac.Volume().
			WithName(configMaps[i].Name).
			WithConfigMap(corev1ac.ConfigMapVolumeSource().
				WithName(configMaps[i].Name)))
	}
	return vols
}
```

No new imports needed -- `corev1` and `corev1ac` are already imported in `objects_common.go`.

- [ ] **Step 9: Run tests to verify they pass**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -run "TestWithFlowSourcePath|TestWithFlowVolumeMounts|TestFlowVolumes" -v -count=1`
Expected: PASS — all 8 new unit tests pass

- [ ] **Step 10: Run all existing tests to check for regressions**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -v -count=1`
Expected: PASS — no regressions in existing tests

- [ ] **Step 11: Commit**

```bash
git add internal/controller/quarkus_constants.go internal/controller/quarkus_config.go internal/controller/quarkus_config_test.go internal/controller/objects_common.go
git commit -m "feat: add volume builder functions and source path ContainerOption for ConfigMap integration"
```

---

### Task 2: Controller Reconcile changes, ConfigMap watch, and mapper

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go:16-188` (full file — add imports, modify Reconcile, modify applyDeployment, modify updateStatus, remove applyConfigMap stub, add listConfigMaps, add mapper, add predicate, update SetupWithManager)

**Interfaces:**
- Consumes:
  - `WithFlowSourcePath() ContainerOption` from Task 1
  - `WithFlowVolumeMounts(configMaps []corev1.ConfigMap) ContainerOption` from Task 1
  - `FlowVolumes(configMaps []corev1.ConfigMap) []*corev1ac.VolumeApplyConfiguration` from Task 1
  - `LabelRuntimeRef` constant from `objects_common.go` — `"logic.kubesmarts.org/runtime-ref"`
  - `LabelWorkflowName` constant from `objects_common.go` — `"logic.kubesmarts.org/workflow-name"`
  - `LabelWorkflowVersion` constant from `objects_common.go` — `"logic.kubesmarts.org/workflow-version"`
  - `logicv1.RuntimeDefinitionStatus` struct from `api/v1/logicflowruntime_types.go:81-91` — fields: Name, Service, Version
- Produces:
  - Modified `Reconcile` that lists ConfigMaps and passes them through
  - Modified `applyDeployment` that accepts `configMaps []corev1.ConfigMap` and injects volumes
  - Modified `updateStatus` that populates `status.definitions[]` and `status.configMapRefs[]`
  - `listConfigMaps(ctx, rt) ([]corev1.ConfigMap, error)` — lists and sorts ConfigMaps by name
  - `mapConfigMapToRuntime(ctx, obj) []reconcile.Request` — maps ConfigMap to Runtime reconcile request
  - `runtimeRefLabelPredicate() predicate.Predicate` — filters ConfigMap events to those with runtime-ref label
  - Updated `SetupWithManager` with `Watches(&corev1.ConfigMap{}, ...)` call

- [ ] **Step 1: Replace the full controller file**

Replace the entire contents of `internal/controller/logicflowruntime_controller.go` with:

```go
package controller

import (
	"context"
	"sort"

	logicv1 "github.com/kubesmarts/logic-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type LogicFlowRuntimeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logic.kubesmarts.org,resources=logicflowruntimes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *LogicFlowRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var rt logicv1.LogicFlowRuntime
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	configMaps, err := r.listConfigMaps(ctx, &rt)
	if err != nil {
		log.Error(err, "failed to list ConfigMaps")
		return ctrl.Result{}, err
	}

	if err := r.applyDeployment(ctx, &rt, configMaps); err != nil {
		log.Error(err, "failed to apply Deployment")
	}

	if err := r.applyService(ctx, &rt); err != nil {
		log.Error(err, "failed to apply Service")
	}

	if err := r.updateStatus(ctx, &rt, configMaps); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LogicFlowRuntimeReconciler) listConfigMaps(ctx context.Context, rt *logicv1.LogicFlowRuntime) ([]corev1.ConfigMap, error) {
	var cmList corev1.ConfigMapList
	if err := r.List(ctx, &cmList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelRuntimeRef: rt.Name},
	); err != nil {
		return nil, err
	}
	sort.Slice(cmList.Items, func(i, j int) bool {
		return cmList.Items[i].Name < cmList.Items[j].Name
	})
	return cmList.Items, nil
}

func (r *LogicFlowRuntimeReconciler) applyDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap) error {
	childLabels := ChildLabels(rt)
	spec := ToDeploymentSpec(
		ContainerNameRunner,
		&rt.Spec.ApplicationSpec,
		childLabels,
		SelectorLabels(rt.Name),
		DefaultRunnerImage(rt.Spec.Persistence),
		WithPersistenceEnvVars(rt.Spec.Persistence, rt.Namespace),
		WithSecurityEnvVars(rt.Spec.Security),
		DefaultProbes(),
		WithFlowSourcePath(),
		WithFlowVolumeMounts(configMaps),
	)
	if len(configMaps) > 0 {
		spec.Template.Spec.WithVolumes(FlowVolumes(configMaps)...)
	}
	deployment := appsv1ac.Deployment(rt.Name, rt.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(rt, logicv1.LogicFlowRuntimeKind)).
		WithSpec(spec)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) applyService(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	svc := QuarkusService(rt, logicv1.LogicFlowRuntimeKind)
	return r.Apply(ctx, svc, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}

func (r *LogicFlowRuntimeReconciler) updateStatus(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap) error {
	rt.Status.ObservedGeneration = rt.Generation
	rt.Status.DeploymentRef.Name = rt.Name
	rt.Status.ServiceRef.Name = rt.Name
	rt.Status.Selector = labels.Set(SelectorLabels(rt.Name)).String()

	rt.Status.ConfigMapRefs = configMapRefs(configMaps)
	rt.Status.Definitions = definitionsFromConfigMaps(configMaps)

	if err := r.updateStatusDeployment(ctx, rt); err != nil {
		return err
	}
	if err := r.updateStatusSvc(ctx, rt); err != nil {
		return err
	}

	rt.Status.Phase = logicv1.DerivePhase(rt.Status.Conditions, rt.Status.ReadyReplicas)

	return r.Status().Update(ctx, rt)
}

func configMapRefs(configMaps []corev1.ConfigMap) []corev1.LocalObjectReference {
	refs := make([]corev1.LocalObjectReference, 0, len(configMaps))
	for i := range configMaps {
		refs = append(refs, corev1.LocalObjectReference{Name: configMaps[i].Name})
	}
	return refs
}

func definitionsFromConfigMaps(configMaps []corev1.ConfigMap) []logicv1.RuntimeDefinitionStatus {
	defs := make([]logicv1.RuntimeDefinitionStatus, 0, len(configMaps))
	for i := range configMaps {
		name := configMaps[i].Labels[LabelWorkflowName]
		if name == "" {
			continue
		}
		defs = append(defs, logicv1.RuntimeDefinitionStatus{
			Name:    name,
			Version: configMaps[i].Labels[LabelWorkflowVersion],
			Service: "/" + name,
		})
	}
	return defs
}

func (r *LogicFlowRuntimeReconciler) updateStatusDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	var deployment appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(rt), &deployment)
	if apierrors.IsNotFound(err) {
		rt.Status.ReadyReplicas = 0
		rt.Status.Replicas = 0
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, logicv1.ReasonDeploymentNotFound, "")
		return nil
	}
	if err != nil {
		return err
	}

	rt.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	rt.Status.Replicas = deployment.Status.Replicas
	logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, logicv1.ReasonDeploymentProgressing, "")

	for _, cond := range deployment.Status.Conditions {
		switch cond.Type {
		case appsv1.DeploymentAvailable:
			if cond.Status == corev1.ConditionTrue {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionTrue, rt.Generation, cond.Reason, cond.Message)
			} else if cond.Status == corev1.ConditionFalse {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, cond.Reason, cond.Message)
			}
			break
		case appsv1.DeploymentProgressing:
			if cond.Status == corev1.ConditionFalse && cond.Reason == logicv1.ReasonProgressDeadlineExceeded {
				logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionDeploymentAvailable, metav1.ConditionFalse, rt.Generation, cond.Reason, cond.Message)
			}
			break
		}
	}

	return nil
}

func (r *LogicFlowRuntimeReconciler) updateStatusSvc(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	var svc corev1.Service
	err := r.Get(ctx, client.ObjectKeyFromObject(rt), &svc)
	if apierrors.IsNotFound(err) {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionServiceReady, metav1.ConditionFalse, rt.Generation, logicv1.ReasonServiceNotFound, "")
		return nil
	}
	if err != nil {
		return err
	}

	logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionServiceReady, metav1.ConditionTrue, rt.Generation, logicv1.ReasonReady, "")
	return nil
}

func (r *LogicFlowRuntimeReconciler) mapConfigMapToRuntime(ctx context.Context, obj client.Object) []reconcile.Request {
	rtName := obj.GetLabels()[LabelRuntimeRef]
	if rtName == "" {
		return nil
	}
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: rtName, Namespace: obj.GetNamespace()}},
	}
}

func runtimeRefLabelPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		_, ok := obj.GetLabels()[LabelRuntimeRef]
		return ok
	})
}

func (r *LogicFlowRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&logicv1.LogicFlowRuntime{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToRuntime),
			builder.WithPredicates(runtimeRefLabelPredicate()),
		).
		Named("logicflowruntime").
		Complete(r)
}
```

Key changes from the existing file:
- **Removed:** `applyConfigMap` stub (lines 76-78)
- **Removed:** `// TODO DefinitionsRef, ConfigMapRef` comment (line 123)
- **Added:** `listConfigMaps` method with sort-by-name
- **Modified:** `Reconcile` now calls `listConfigMaps` and passes result to `applyDeployment` and `updateStatus`
- **Modified:** `applyDeployment` accepts `configMaps []corev1.ConfigMap`, uses `ToDeploymentSpec` result to add volumes after, passes `WithFlowSourcePath()` and `WithFlowVolumeMounts(configMaps)` as ContainerOptions
- **Modified:** `updateStatus` accepts `configMaps []corev1.ConfigMap`, populates `ConfigMapRefs` and `Definitions`
- **Added:** `configMapRefs`, `definitionsFromConfigMaps` helper functions
- **Added:** `mapConfigMapToRuntime` mapper, `runtimeRefLabelPredicate` predicate
- **Modified:** `SetupWithManager` adds `Watches(&corev1.ConfigMap{}, ...)`
- **Added imports:** `"sort"`, `"k8s.io/apimachinery/pkg/types"`, `"sigs.k8s.io/controller-runtime/pkg/builder"`, `"sigs.k8s.io/controller-runtime/pkg/handler"`, `"sigs.k8s.io/controller-runtime/pkg/predicate"`, `"sigs.k8s.io/controller-runtime/pkg/reconcile"`

- [ ] **Step 2: Verify the file compiles**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go build ./internal/controller/...`
Expected: builds without errors

- [ ] **Step 3: Run existing tests to check for regressions**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -v -count=1`
Expected: PASS — existing tests should still pass because:
- The existing "Minimal runtime" test doesn't assert env var count, so adding `QUARKUS_FLOW_RUNNER_SOURCE_PATH` won't break it
- `applyDeployment` now takes an extra `configMaps` param, but existing tests call `r.Reconcile()` not `r.applyDeployment()` directly
- `updateStatus` now takes `configMaps` but same reasoning applies

- [ ] **Step 4: Commit**

```bash
git add internal/controller/logicflowruntime_controller.go
git commit -m "feat: integrate ConfigMap discovery and volume mounting into Runtime controller"
```

---

### Task 3: Controller envtest tests for ConfigMap integration

**Files:**
- Modify: `internal/controller/logicflowruntime_controller_test.go` (add helper functions and new test contexts)

**Interfaces:**
- Consumes:
  - `reconcileAndFetch(ctx, r, nn)` — existing test helper, returns `*logicv1.LogicFlowRuntime`
  - `newReconciler()` — existing test helper, returns `*LogicFlowRuntimeReconciler`
  - `createRuntime(ctx, name, spec)` — existing test helper
  - `deleteRuntime(ctx, nn)` — existing test helper
  - `mainContainer(dep)` — existing test helper, returns the `logic-runner` container
  - `findEnvVar(envs, name)` — existing test helper
  - `ConfigMapPrefix`, `LabelRuntimeRef`, `LabelWorkflowName`, `LabelWorkflowVersion`, `LabelManagedBy`, `LabelPartOf`, `WorkflowMountPath` — constants from `objects_common.go` and `quarkus_constants.go`
- Produces: test coverage for ConfigMap integration

- [ ] **Step 1: Add test helper functions**

In `internal/controller/logicflowruntime_controller_test.go`, add after the `mainContainer` function (line 73) and before `var _ = Describe(...)`:

```go
func createFlowConfigMap(ctx context.Context, name, runtimeRef, workflowName, workflowVersion string) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name":       name,
				"app.kubernetes.io/managed-by": LabelManagedBy,
				"app.kubernetes.io/part-of":    LabelPartOf,
				LabelRuntimeRef:                runtimeRef,
				LabelWorkflowName:              workflowName,
				LabelWorkflowVersion:           workflowVersion,
			},
		},
		Data: map[string]string{
			workflowName + ".json": `{"document":{"name":"` + workflowName + `"}}`,
		},
	}
	Expect(k8sClient.Create(ctx, cm)).To(Succeed())
}

func deleteConfigMap(ctx context.Context, name string) {
	cm := &corev1.ConfigMap{}
	nn := types.NamespacedName{Name: name, Namespace: "default"}
	err := k8sClient.Get(ctx, nn, cm)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
}

func findVolume(dep *appsv1.Deployment, name string) *corev1.Volume {
	for i := range dep.Spec.Template.Spec.Volumes {
		if dep.Spec.Template.Spec.Volumes[i].Name == name {
			return &dep.Spec.Template.Spec.Volumes[i]
		}
	}
	return nil
}

func findVolumeMount(c corev1.Container, name string) *corev1.VolumeMount {
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == name {
			return &c.VolumeMounts[i]
		}
	}
	return nil
}
```

Add `"strings"` to the imports block (needed for `strings.HasPrefix` in the multiple ConfigMaps test).

- [ ] **Step 2: Add test context for source path env var (no ConfigMaps)**

Add inside the `Describe("LogicFlowRuntime Controller", ...)` block, after the existing `Context("Spec updates via SSA", ...)`:

```go
	Context("Source path env var", func() {
		const name = "test-source-path"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteRuntime(ctx, nn)
		})

		It("should always set QUARKUS_FLOW_RUNNER_SOURCE_PATH", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			c := mainContainer(&dep)
			env := findEnvVar(c.Env, "QUARKUS_FLOW_RUNNER_SOURCE_PATH")
			Expect(env).NotTo(BeNil())
			Expect(env.Value).To(Equal(WorkflowMountPath))
		})

		It("should have empty definitions and configMapRefs with no ConfigMaps", func() {
			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())
		})
	})
```

- [ ] **Step 3: Add test context for one ConfigMap**

Add after the "Source path env var" context:

```go
	Context("With one workflow ConfigMap", func() {
		const name = "test-one-cm"
		const cmName = "lfd-payment-processor"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cmName, name, "payment-processor", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should add a volume for the ConfigMap", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			vol := findVolume(&dep, cmName)
			Expect(vol).NotTo(BeNil(), "volume for ConfigMap not found")
			Expect(vol.ConfigMap).NotTo(BeNil())
			Expect(vol.ConfigMap.Name).To(Equal(cmName))
		})

		It("should add a volumeMount for the ConfigMap", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			c := mainContainer(&dep)

			vm := findVolumeMount(c, cmName)
			Expect(vm).NotTo(BeNil(), "volumeMount for ConfigMap not found")
			Expect(vm.MountPath).To(Equal(WorkflowMountPath + "/" + cmName))
			Expect(vm.ReadOnly).To(BeTrue())
		})

		It("should populate status.definitions with workflow metadata", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.Definitions[0].Name).To(Equal("payment-processor"))
			Expect(rt.Status.Definitions[0].Version).To(Equal("1.0.0"))
			Expect(rt.Status.Definitions[0].Service).To(Equal("/payment-processor"))
		})

		It("should populate status.configMapRefs", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.ConfigMapRefs).To(HaveLen(1))
			Expect(rt.Status.ConfigMapRefs[0].Name).To(Equal(cmName))
		})
	})
```

- [ ] **Step 4: Add test context for multiple ConfigMaps**

Add after the "With one workflow ConfigMap" context:

```go
	Context("With multiple workflow ConfigMaps", func() {
		const name = "test-multi-cm"
		const cm1Name = "lfd-order-flow"
		const cm2Name = "lfd-payment-processor"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cm1Name, name, "order-flow", "2.0.0")
			createFlowConfigMap(ctx, cm2Name, name, "payment-processor", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cm1Name)
			deleteConfigMap(ctx, cm2Name)
			deleteRuntime(ctx, nn)
		})

		It("should add volumes sorted by name", func() {
			reconcileAndFetch(ctx, r, nn)

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

			var flowVols []string
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if strings.HasPrefix(v.Name, ConfigMapPrefix) {
					flowVols = append(flowVols, v.Name)
				}
			}
			Expect(flowVols).To(Equal([]string{cm1Name, cm2Name}))
		})

		It("should populate status with all definitions sorted by ConfigMap name", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(HaveLen(2))
			Expect(rt.Status.Definitions[0].Name).To(Equal("order-flow"))
			Expect(rt.Status.Definitions[1].Name).To(Equal("payment-processor"))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(2))
			Expect(rt.Status.ConfigMapRefs[0].Name).To(Equal(cm1Name))
			Expect(rt.Status.ConfigMapRefs[1].Name).To(Equal(cm2Name))
		})
	})
```

- [ ] **Step 5: Add test context for ConfigMap lifecycle (add/remove)**

Add after the "With multiple workflow ConfigMaps" context:

```go
	Context("ConfigMap lifecycle", func() {
		const name = "test-cm-lifecycle"
		const cmName = "lfd-lifecycle-wf"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should add volume when ConfigMap appears after initial reconcile", func() {
			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Definitions).To(BeEmpty())

			createFlowConfigMap(ctx, cmName, name, "lifecycle-wf", "1.0.0")
			rt = reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(HaveLen(1))
			Expect(rt.Status.ConfigMapRefs).To(HaveLen(1))

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).NotTo(BeNil())
		})

		It("should remove volume when ConfigMap is deleted", func() {
			createFlowConfigMap(ctx, cmName, name, "lifecycle-wf", "1.0.0")
			rt := reconcileAndFetch(ctx, r, nn)
			Expect(rt.Status.Definitions).To(HaveLen(1))

			deleteConfigMap(ctx, cmName)
			rt = reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).To(BeNil())
		})
	})
```

- [ ] **Step 6: Add test context for ConfigMap with wrong runtime-ref**

Add after the "ConfigMap lifecycle" context:

```go
	Context("ConfigMap with wrong runtime-ref", func() {
		const name = "test-wrong-ref"
		const cmName = "lfd-wrong-ref-wf"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cmName, "other-runtime", "wrong-ref-wf", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should not include ConfigMap in volumes or status", func() {
			rt := reconcileAndFetch(ctx, r, nn)

			Expect(rt.Status.Definitions).To(BeEmpty())
			Expect(rt.Status.ConfigMapRefs).To(BeEmpty())

			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
			Expect(findVolume(&dep, cmName)).To(BeNil())
		})
	})
```

- [ ] **Step 7: Add idempotency test with ConfigMaps**

The existing "Idempotency" context tests the no-ConfigMap case. Add a new test verifying that repeated reconciliation with ConfigMaps produces identical results. Add after the "ConfigMap with wrong runtime-ref" context:

```go
	Context("Idempotency with ConfigMaps", func() {
		const name = "test-idem-cm"
		const cmName = "lfd-idem-wf"
		var nn types.NamespacedName
		var r *LogicFlowRuntimeReconciler

		BeforeEach(func() {
			r = newReconciler()
			nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
			createFlowConfigMap(ctx, cmName, name, "idem-wf", "1.0.0")
		})
		AfterEach(func() {
			deleteConfigMap(ctx, cmName)
			deleteRuntime(ctx, nn)
		})

		It("should produce the same result on repeated reconciliation with ConfigMaps", func() {
			rt1 := reconcileAndFetch(ctx, r, nn)
			Expect(rt1.Status.Definitions).To(HaveLen(1))

			rt2 := reconcileAndFetch(ctx, r, nn)
			Expect(rt2.Status.Definitions).To(HaveLen(1))
			Expect(rt2.Status.Definitions[0].Name).To(Equal(rt1.Status.Definitions[0].Name))
			Expect(rt2.Status.ConfigMapRefs).To(HaveLen(1))
			Expect(rt2.Status.ConfigMapRefs[0].Name).To(Equal(rt1.Status.ConfigMapRefs[0].Name))
		})
	})
```

- [ ] **Step 8: Run all tests to verify everything passes**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && go test ./internal/controller/ -v -count=1`
Expected: PASS — all existing tests + all new tests pass

- [ ] **Step 9: Run `make generate manifests` to verify no RBAC changes needed**

Run: `cd /Users/ricferna/dev/github/kubesmarts/logic-operator && make generate manifests`
Expected: no changes to `config/rbac/role.yaml` (ConfigMap read RBAC already exists)

Verify: `git diff config/rbac/role.yaml` should show no diff.

- [ ] **Step 10: Commit**

```bash
git add internal/controller/logicflowruntime_controller_test.go
git commit -m "test: add envtest coverage for Runtime ConfigMap integration"
```
