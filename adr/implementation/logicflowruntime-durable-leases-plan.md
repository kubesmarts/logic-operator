# Durable Lease Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pre-create Kubernetes Leases in the LogicFlowRuntime controller so the Quarkus Flow durable extension can acquire them without leader election.

**Architecture:** When persistence is configured, the Runtime controller creates member leases matching the replica count, sets durable env vars (disabling leader election, setting pool name), creates pod RBAC (ServiceAccount + RoleBinding bound to a shared ClusterRole), and updates status with lease count and a LeaseReady condition. Leases are standard Create/Delete (not SSA) to avoid conflicting with pod-side updates.

**Tech Stack:** Go, controller-runtime v0.24.1, Kubernetes coordination/v1 Lease API, rbac/v1 for pod RBAC, envtest + Ginkgo v2 for integration tests, standard `testing` + gomega for unit tests.

## Global Constraints

- Lease management activates ONLY when `rt.Spec.Persistence != nil`
- Lease names follow `flow-pool-member-{rt.Name}-{NN}` (NN zero-padded)
- Pool name is immutable and equals `rt.Name` — user duplicates filtered
- Leader lease is never created — operator replaces leader entirely
- Leases use standard Create/Delete, NOT Server-Side Apply
- `AlreadyExists` on Create and `NotFound` on Delete are ignored (idempotent)
- Owner reference on leases points to the Deployment with `controller=false`
- Owner reference on ServiceAccount/RoleBinding points to the Runtime CR with `controller=true`
- ClusterRole `logic-flow-runtime-durable` is shared, has no owner reference
- No license headers on new files; preserve existing headers on modified files
- No git commits — user handles all commits per CLAUDE.md
- Unit tests: standard `testing` + `gomega.NewWithT(t)` pattern
- Envtest tests: Ginkgo v2 + Gomega dot imports, matching existing controller test patterns

---

### Task 1: Constants, types, and pure functions

**Files:**
- Modify: `internal/controller/quarkus_constants.go`
- Modify: `api/v1/status_types.go`
- Modify: `api/v1/logicflowruntime_types.go`
- Modify: `internal/controller/objects_common.go`
- Modify: `internal/controller/quarkus_config.go`
- Test: `internal/controller/quarkus_config_test.go`

**Interfaces:**
- Consumes: `envLiteral` from `quarkus_config.go`, `logicv1.ApplicationSpec` from `api/v1`
- Produces:
  - Constants: `LeaseMemberNameFmt string`, `LeaseDuration int32`, `LabelDurablePool string`, `LabelDurableIsLeader string`, `DurableComponentValue string`, `DurableManagedByValue string`, `ClusterRoleDurable string`
  - Types: `logicv1.ConditionLeaseReady string`, `logicv1.ReasonLeaseNotFound string`, `LeaseReplicas int32` field on `LogicFlowRuntimeStatus`
  - Functions: `effectiveReplicas(app *logicv1.ApplicationSpec) int32`, `WithDurableEnvVars(rt *logicv1.LogicFlowRuntime) ContainerOption`, `envFieldRef(name, fieldPath string) *corev1ac.EnvVarApplyConfiguration`, `newMemberLease(name, namespace, poolName string, dep *appsv1.Deployment) *coordinationv1.Lease`, `memberLeaseLabels(poolName string) map[string]string`

- [ ] **Step 1: Add durable constants to `quarkus_constants.go`**

Append after the `WorkflowMountPath` constant:

```go
LeaseMemberNameFmt  = "flow-pool-member-%s-%02d"
LeaseDuration       = int32(30)

LabelDurablePool     = "io.quarkiverse.flow.durable.k8s/pool"
LabelDurableIsLeader = "io.quarkiverse.flow.durable.k8s/is-leader"

DurableComponentValue = "durable"
DurableManagedByValue = "quarkus-flow"

ClusterRoleDurable = "logic-flow-runtime-durable"
```

- [ ] **Step 2: Add condition and reason constants to `api/v1/status_types.go`**

Add `ConditionLeaseReady` to the Runtime conditions block (after `ConditionServiceReady`):

```go
ConditionLeaseReady = "LeaseReady"
```

Add `ReasonLeaseNotFound` to the Runtime reasons block (after `ReasonProgressDeadlineExceeded`):

```go
ReasonLeaseNotFound = "LeaseNotFound"
```

- [ ] **Step 3: Add `LeaseReplicas` field to `LogicFlowRuntimeStatus`**

In `api/v1/logicflowruntime_types.go`, add after the `ConfigMapRefs` field:

```go
// LeaseReplicas is the number of durable pool leases.
// +optional
LeaseReplicas int32 `json:"leaseReplicas,omitempty"`
```

- [ ] **Step 4: Add `effectiveReplicas` to `objects_common.go`**

Add after `FlowVolumes`:

```go
func effectiveReplicas(app *logicv1.ApplicationSpec) int32 {
	if app.PodTemplate.Replicas != nil {
		return *app.PodTemplate.Replicas
	}
	if app.Replicas != nil {
		return *app.Replicas
	}
	return 1
}
```

- [ ] **Step 5: Add `newMemberLease` and `memberLeaseLabels` to `objects_common.go`**

Add after `effectiveReplicas`. Requires new import `coordinationv1 "k8s.io/api/coordination/v1"` and `appsv1 "k8s.io/api/apps/v1"`:

```go
func memberLeaseLabels(poolName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": DurableManagedByValue,
		"app.kubernetes.io/component":  DurableComponentValue,
		LabelDurablePool:               poolName,
		LabelDurableIsLeader:           "false",
	}
}

func newMemberLease(name, namespace, poolName string, dep *appsv1.Deployment) *coordinationv1.Lease {
	duration := LeaseDuration
	controller := false
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    memberLeaseLabels(poolName),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       dep.Name,
					UID:        dep.UID,
					Controller: &controller,
				},
			},
		},
		Spec: coordinationv1.LeaseSpec{
			LeaseDurationSeconds: &duration,
		},
	}
}
```

- [ ] **Step 6: Add `envFieldRef` helper to `quarkus_config.go`**

Add after `envFromSecret`:

```go
func envFieldRef(name, fieldPath string) *corev1ac.EnvVarApplyConfiguration {
	return corev1ac.EnvVar().
		WithName(name).
		WithValueFrom(corev1ac.EnvVarSource().
			WithFieldRef(corev1ac.ObjectFieldSelector().
				WithFieldPath(fieldPath)))
}
```

- [ ] **Step 7: Add `WithDurableEnvVars` to `quarkus_config.go`**

Add after `WithFlowVolumeMounts`:

```go
func WithDurableEnvVars(rt *logicv1.LogicFlowRuntime) ContainerOption {
	return func(c *corev1ac.ContainerApplyConfiguration) {
		immutable := map[string]bool{
			"QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED": true,
			"QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME":            true,
			"POD_NAME":      true,
			"POD_NAMESPACE": true,
		}
		filtered := make([]*corev1ac.EnvVarApplyConfiguration, 0, len(c.Env))
		for _, e := range c.Env {
			if e.Name != nil && immutable[*e.Name] {
				continue
			}
			filtered = append(filtered, e)
		}
		c.Env = filtered
		c.WithEnv(
			envLiteral("QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED", "false"),
			envLiteral("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME", rt.Name),
			envFieldRef("POD_NAME", "metadata.name"),
			envFieldRef("POD_NAMESPACE", "metadata.namespace"),
		)
	}
}
```

- [ ] **Step 8: Write unit tests**

Add to `internal/controller/quarkus_config_test.go`. Requires new import `coordinationv1 "k8s.io/api/coordination/v1"` and `appsv1 "k8s.io/api/apps/v1"`:

```go
func int32Ptr(i int32) *int32 { return &i }

func TestEffectiveReplicas_Default(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{}
	g.Expect(effectiveReplicas(app)).To(gomega.Equal(int32(1)))
}

func TestEffectiveReplicas_ExplicitReplicas(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{Replicas: int32Ptr(3)}
	g.Expect(effectiveReplicas(app)).To(gomega.Equal(int32(3)))
}

func TestEffectiveReplicas_PodTemplateOverrides(t *testing.T) {
	g := gomega.NewWithT(t)
	app := &logicv1.ApplicationSpec{
		Replicas: int32Ptr(3),
		PodTemplate: logicv1.PodTemplateSpec{Replicas: int32Ptr(5)},
	}
	g.Expect(effectiveReplicas(app)).To(gomega.Equal(int32(5)))
}

func TestWithDurableEnvVars_SetsAllEnvVars(t *testing.T) {
	g := gomega.NewWithT(t)
	rt := &logicv1.LogicFlowRuntime{}
	rt.Name = "my-runtime"
	c := corev1ac.Container().WithName("test")

	WithDurableEnvVars(rt)(c)

	g.Expect(c.Env).To(gomega.HaveLen(4))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED"))
	g.Expect(*c.Env[0].Value).To(gomega.Equal("false"))
	g.Expect(*c.Env[1].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME"))
	g.Expect(*c.Env[1].Value).To(gomega.Equal("my-runtime"))
	g.Expect(*c.Env[2].Name).To(gomega.Equal("POD_NAME"))
	g.Expect(*c.Env[2].ValueFrom.FieldRef.FieldPath).To(gomega.Equal("metadata.name"))
	g.Expect(*c.Env[3].Name).To(gomega.Equal("POD_NAMESPACE"))
	g.Expect(*c.Env[3].ValueFrom.FieldRef.FieldPath).To(gomega.Equal("metadata.namespace"))
}

func TestWithDurableEnvVars_FiltersUserDuplicates(t *testing.T) {
	g := gomega.NewWithT(t)
	rt := &logicv1.LogicFlowRuntime{}
	rt.Name = "my-runtime"
	c := corev1ac.Container().WithName("test").
		WithEnv(
			corev1ac.EnvVar().WithName("OTHER_VAR").WithValue("keep"),
			corev1ac.EnvVar().WithName("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME").WithValue("user-pool"),
			corev1ac.EnvVar().WithName("POD_NAME").WithValue("user-pod"),
		)

	WithDurableEnvVars(rt)(c)

	g.Expect(c.Env).To(gomega.HaveLen(5))
	g.Expect(*c.Env[0].Name).To(gomega.Equal("OTHER_VAR"))
	g.Expect(*c.Env[0].Value).To(gomega.Equal("keep"))
	// Operator values after OTHER_VAR
	g.Expect(*c.Env[1].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED"))
	g.Expect(*c.Env[2].Name).To(gomega.Equal("QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME"))
	g.Expect(*c.Env[2].Value).To(gomega.Equal("my-runtime"))
	g.Expect(*c.Env[3].Name).To(gomega.Equal("POD_NAME"))
	g.Expect(*c.Env[4].Name).To(gomega.Equal("POD_NAMESPACE"))
}

func TestNewMemberLease_Fields(t *testing.T) {
	g := gomega.NewWithT(t)
	dep := &appsv1.Deployment{}
	dep.Name = "my-runtime"
	dep.UID = "dep-uid-123"

	lease := newMemberLease("flow-pool-member-my-runtime-00", "default", "my-runtime", dep)

	g.Expect(lease.Name).To(gomega.Equal("flow-pool-member-my-runtime-00"))
	g.Expect(lease.Namespace).To(gomega.Equal("default"))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/managed-by", DurableManagedByValue))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue("app.kubernetes.io/component", DurableComponentValue))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue(LabelDurablePool, "my-runtime"))
	g.Expect(lease.Labels).To(gomega.HaveKeyWithValue(LabelDurableIsLeader, "false"))
	g.Expect(*lease.Spec.LeaseDurationSeconds).To(gomega.Equal(LeaseDuration))
	g.Expect(lease.Spec.HolderIdentity).To(gomega.BeNil())
	g.Expect(lease.OwnerReferences).To(gomega.HaveLen(1))
	g.Expect(lease.OwnerReferences[0].Kind).To(gomega.Equal("Deployment"))
	g.Expect(lease.OwnerReferences[0].Name).To(gomega.Equal("my-runtime"))
	g.Expect(*lease.OwnerReferences[0].Controller).To(gomega.BeFalse())
}

func TestMemberLeaseLabels(t *testing.T) {
	g := gomega.NewWithT(t)
	labels := memberLeaseLabels("my-pool")
	g.Expect(labels).To(gomega.HaveLen(4))
	g.Expect(labels[LabelDurablePool]).To(gomega.Equal("my-pool"))
	g.Expect(labels[LabelDurableIsLeader]).To(gomega.Equal("false"))
}
```

- [ ] **Step 9: Run unit tests**

Run: `go test ./internal/controller/ -run "TestEffectiveReplicas|TestWithDurableEnvVars|TestNewMemberLease|TestMemberLeaseLabels" -v`
Expected: All PASS

- [ ] **Step 10: Run full test suite**

Run: `go test ./internal/controller/ -v -count=1`
Expected: All specs pass (52 existing + no regressions)

---

### Task 2: Lease lifecycle and controller integration

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go`
- Test: `internal/controller/logicflowruntime_controller_test.go`

**Interfaces:**
- Consumes: `effectiveReplicas(app *logicv1.ApplicationSpec) int32`, `newMemberLease(name, namespace, poolName string, dep *appsv1.Deployment) *coordinationv1.Lease`, `memberLeaseLabels(poolName string) map[string]string`, `WithDurableEnvVars(rt *logicv1.LogicFlowRuntime) ContainerOption`, `LeaseMemberNameFmt`, `LabelDurablePool`, `ConditionLeaseReady`, `ReasonLeaseNotFound`, `ReasonReady`
- Produces: `reconcileLeases(ctx, rt) error`, `updateStatusLeases(ctx, rt) error` — both methods on `LogicFlowRuntimeReconciler`

- [ ] **Step 1: Add RBAC markers and new imports to the controller**

Add RBAC marker after the existing `configmaps` marker in `logicflowruntime_controller.go`:

```go
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;delete
```

Add new imports:

```go
coordinationv1 "k8s.io/api/coordination/v1"
```

- [ ] **Step 2: Add `reconcileLeases` method**

Add after `listConfigMaps`:

```go
func (r *LogicFlowRuntimeReconciler) reconcileLeases(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	if rt.Spec.Persistence == nil {
		return nil
	}

	desired := effectiveReplicas(&rt.Spec.ApplicationSpec)

	var dep appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(rt), &dep); err != nil {
		return err
	}

	var leaseList coordinationv1.LeaseList
	if err := r.List(ctx, &leaseList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelDurablePool: rt.Name},
	); err != nil {
		return err
	}

	existing := make(map[string]struct{}, len(leaseList.Items))
	for i := range leaseList.Items {
		existing[leaseList.Items[i].Name] = struct{}{}
	}

	desiredNames := make(map[string]struct{}, desired)
	for i := int32(0); i < desired; i++ {
		name := fmt.Sprintf(LeaseMemberNameFmt, rt.Name, i)
		desiredNames[name] = struct{}{}
		if _, ok := existing[name]; ok {
			continue
		}
		lease := newMemberLease(name, rt.Namespace, rt.Name, &dep)
		if err := r.Create(ctx, lease); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	for i := range leaseList.Items {
		if _, ok := desiredNames[leaseList.Items[i].Name]; !ok {
			if err := r.Delete(ctx, &leaseList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}
```

- [ ] **Step 3: Add `updateStatusLeases` method**

Add after `updateStatusSvc`:

```go
func (r *LogicFlowRuntimeReconciler) updateStatusLeases(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	if rt.Spec.Persistence == nil {
		return nil
	}

	var leaseList coordinationv1.LeaseList
	if err := r.List(ctx, &leaseList,
		client.InNamespace(rt.Namespace),
		client.MatchingLabels{LabelDurablePool: rt.Name},
	); err != nil {
		return err
	}

	rt.Status.LeaseReplicas = int32(len(leaseList.Items))
	desired := effectiveReplicas(&rt.Spec.ApplicationSpec)

	if rt.Status.LeaseReplicas >= desired {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionLeaseReady, metav1.ConditionTrue, rt.Generation, logicv1.ReasonReady, "")
	} else {
		logicv1.SetCondition(&rt.Status.Conditions, logicv1.ConditionLeaseReady, metav1.ConditionFalse, rt.Generation, logicv1.ReasonLeaseNotFound,
			fmt.Sprintf("%d of %d leases ready", rt.Status.LeaseReplicas, desired))
	}

	return nil
}
```

- [ ] **Step 4: Update `applyDeployment` to include durable env vars and serviceAccountName**

In `applyDeployment`, add `WithDurableEnvVars` to the `ToDeploymentSpec` call when persistence is configured. Change the function to conditionally add the ContainerOption and set serviceAccountName on the pod spec.

Replace the current `applyDeployment` method. The key changes are:
1. Build `opts` slice conditionally — append `WithDurableEnvVars(rt)` when persistence is set
2. After `ToDeploymentSpec`, set `spec.Template.Spec.WithServiceAccountName(rt.Name)` when persistence is set

```go
func (r *LogicFlowRuntimeReconciler) applyDeployment(ctx context.Context, rt *logicv1.LogicFlowRuntime, configMaps []corev1.ConfigMap) error {
	childLabels := ChildLabels(rt)
	opts := []ContainerOption{
		DefaultRunnerImage(rt.Spec.Persistence),
		WithPersistenceEnvVars(rt.Spec.Persistence, rt.Namespace),
		WithSecurityEnvVars(rt.Spec.Security),
		DefaultProbes(),
		WithFlowSourcePath(),
		WithFlowVolumeMounts(configMaps),
	}
	if rt.Spec.Persistence != nil {
		opts = append(opts, WithDurableEnvVars(rt))
	}
	spec := ToDeploymentSpec(
		ContainerNameRunner,
		&rt.Spec.ApplicationSpec,
		childLabels,
		SelectorLabels(rt.Name),
		opts...,
	)
	if len(configMaps) > 0 {
		spec.Template.Spec.WithVolumes(FlowVolumes(configMaps)...)
	}
	if rt.Spec.Persistence != nil {
		spec.Template.Spec.WithServiceAccountName(rt.Name)
	}
	deployment := appsv1ac.Deployment(rt.Name, rt.Namespace).
		WithLabels(childLabels).
		WithOwnerReferences(OwnerRef(rt, logicv1.LogicFlowRuntimeKind)).
		WithSpec(spec)

	return r.Apply(ctx, deployment, client.FieldOwner(FieldOwnerLogicOperator), client.ForceOwnership)
}
```

- [ ] **Step 5: Update `Reconcile` to call `reconcileLeases`**

In the `Reconcile` method, add the `reconcileLeases` call after `applyDeployment`:

```go
if err := r.reconcileLeases(ctx, &rt); err != nil {
	log.Error(err, "failed to reconcile leases")
}
```

- [ ] **Step 6: Update `updateStatus` to call `updateStatusLeases`**

In the `updateStatus` method, add after `updateStatusSvc`:

```go
if err := r.updateStatusLeases(ctx, rt, configMaps); err != nil {
	return err
}
```

Wait — `updateStatusLeases` doesn't take `configMaps`. Correct call:

```go
if err := r.updateStatusLeases(ctx, rt); err != nil {
	return err
}
```

Add this before the `DerivePhase` call.

- [ ] **Step 7: Write envtest tests for lease creation**

Add to `internal/controller/logicflowruntime_controller_test.go`. Add new import `coordinationv1 "k8s.io/api/coordination/v1"`.

Add helper functions:

```go
func persistenceSpec() logicv1.LogicFlowRuntimeSpec {
	return logicv1.LogicFlowRuntimeSpec{
		RuntimeSpec: logicv1.RuntimeSpec{
			Persistence: &logicv1.PersistenceOptionsSpec{
				PostgreSQL: &logicv1.PersistencePostgreSQL{
					SecretRef: logicv1.PostgreSQLSecretOptions{Name: "pg-creds"},
					JdbcUrl:   "jdbc:postgresql://pg.default.svc:5432/logicflow",
				},
			},
		},
	}
}

func listLeases(ctx context.Context, poolName string) []coordinationv1.Lease {
	var list coordinationv1.LeaseList
	err := k8sClient.List(ctx, &list,
		client.InNamespace("default"),
		client.MatchingLabels{LabelDurablePool: poolName},
	)
	Expect(err).NotTo(HaveOccurred())
	return list.Items
}

func deleteLeases(ctx context.Context, poolName string) {
	leases := listLeases(ctx, poolName)
	for i := range leases {
		_ = k8sClient.Delete(ctx, &leases[i])
	}
}
```

Add test context:

```go
Context("Lease creation with persistence", func() {
	const name = "test-lease"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, persistenceSpec())
	})
	AfterEach(func() {
		deleteLeases(ctx, name)
		deleteRuntime(ctx, nn)
	})

	It("should create one lease for default replicas", func() {
		reconcileAndFetch(ctx, r, nn)

		leases := listLeases(ctx, name)
		Expect(leases).To(HaveLen(1))
		Expect(leases[0].Name).To(Equal(fmt.Sprintf(LeaseMemberNameFmt, name, 0)))
		Expect(*leases[0].Spec.LeaseDurationSeconds).To(Equal(LeaseDuration))
		Expect(leases[0].Spec.HolderIdentity).To(BeNil())
	})

	It("should set correct labels on leases", func() {
		reconcileAndFetch(ctx, r, nn)

		leases := listLeases(ctx, name)
		Expect(leases[0].Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", DurableManagedByValue))
		Expect(leases[0].Labels).To(HaveKeyWithValue("app.kubernetes.io/component", DurableComponentValue))
		Expect(leases[0].Labels).To(HaveKeyWithValue(LabelDurablePool, name))
		Expect(leases[0].Labels).To(HaveKeyWithValue(LabelDurableIsLeader, "false"))
	})

	It("should set Deployment owner reference on leases", func() {
		reconcileAndFetch(ctx, r, nn)

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())

		leases := listLeases(ctx, name)
		Expect(leases[0].OwnerReferences).To(HaveLen(1))
		Expect(leases[0].OwnerReferences[0].Kind).To(Equal("Deployment"))
		Expect(leases[0].OwnerReferences[0].Name).To(Equal(name))
		Expect(leases[0].OwnerReferences[0].UID).To(Equal(dep.UID))
		Expect(*leases[0].OwnerReferences[0].Controller).To(BeFalse())
	})

	It("should set LeaseReady condition and LeaseReplicas", func() {
		rt := reconcileAndFetch(ctx, r, nn)

		Expect(rt.Status.LeaseReplicas).To(Equal(int32(1)))

		leaseCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionLeaseReady)
		Expect(leaseCond).NotTo(BeNil())
		Expect(leaseCond.Status).To(Equal(metav1.ConditionTrue))
	})
})

Context("No leases without persistence", func() {
	const name = "test-no-lease"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
	})
	AfterEach(func() {
		deleteRuntime(ctx, nn)
	})

	It("should not create any leases", func() {
		reconcileAndFetch(ctx, r, nn)

		leases := listLeases(ctx, name)
		Expect(leases).To(BeEmpty())
	})

	It("should not set LeaseReady condition", func() {
		rt := reconcileAndFetch(ctx, r, nn)

		leaseCond := meta.FindStatusCondition(rt.Status.Conditions, logicv1.ConditionLeaseReady)
		Expect(leaseCond).To(BeNil())
	})
})

Context("Lease scaling", func() {
	const name = "test-lease-scale"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, persistenceSpec())
	})
	AfterEach(func() {
		deleteLeases(ctx, name)
		deleteRuntime(ctx, nn)
	})

	It("should create additional leases on scale up", func() {
		reconcileAndFetch(ctx, r, nn)
		Expect(listLeases(ctx, name)).To(HaveLen(1))

		var rt logicv1.LogicFlowRuntime
		Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
		replicas := int32(3)
		rt.Spec.Replicas = &replicas
		Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

		rt2 := reconcileAndFetch(ctx, r, nn)
		leases := listLeases(ctx, name)
		Expect(leases).To(HaveLen(3))
		Expect(rt2.Status.LeaseReplicas).To(Equal(int32(3)))
	})

	It("should delete excess leases on scale down", func() {
		var rt logicv1.LogicFlowRuntime
		Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
		replicas := int32(3)
		rt.Spec.Replicas = &replicas
		Expect(k8sClient.Update(ctx, &rt)).To(Succeed())
		reconcileAndFetch(ctx, r, nn)
		Expect(listLeases(ctx, name)).To(HaveLen(3))

		Expect(k8sClient.Get(ctx, nn, &rt)).To(Succeed())
		replicas = int32(1)
		rt.Spec.Replicas = &replicas
		Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

		rt2 := reconcileAndFetch(ctx, r, nn)
		leases := listLeases(ctx, name)
		Expect(leases).To(HaveLen(1))
		Expect(leases[0].Name).To(Equal(fmt.Sprintf(LeaseMemberNameFmt, name, 0)))
		Expect(rt2.Status.LeaseReplicas).To(Equal(int32(1)))
	})
})

Context("Lease idempotency", func() {
	const name = "test-lease-idem"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, persistenceSpec())
	})
	AfterEach(func() {
		deleteLeases(ctx, name)
		deleteRuntime(ctx, nn)
	})

	It("should produce the same leases on repeated reconciliation", func() {
		reconcileAndFetch(ctx, r, nn)
		leases1 := listLeases(ctx, name)
		Expect(leases1).To(HaveLen(1))

		reconcileAndFetch(ctx, r, nn)
		leases2 := listLeases(ctx, name)
		Expect(leases2).To(HaveLen(1))
		Expect(leases2[0].Name).To(Equal(leases1[0].Name))
		Expect(leases2[0].UID).To(Equal(leases1[0].UID))
	})
})

Context("Durable env vars on Deployment", func() {
	const name = "test-durable-env"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, persistenceSpec())
	})
	AfterEach(func() {
		deleteLeases(ctx, name)
		deleteRuntime(ctx, nn)
	})

	It("should set durable env vars when persistence is configured", func() {
		reconcileAndFetch(ctx, r, nn)

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
		c := mainContainer(&dep)

		leader := findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED")
		Expect(leader).NotTo(BeNil())
		Expect(leader.Value).To(Equal("false"))

		pool := findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME")
		Expect(pool).NotTo(BeNil())
		Expect(pool.Value).To(Equal(name))

		podName := findEnvVar(c.Env, "POD_NAME")
		Expect(podName).NotTo(BeNil())
		Expect(podName.ValueFrom.FieldRef.FieldPath).To(Equal("metadata.name"))

		podNs := findEnvVar(c.Env, "POD_NAMESPACE")
		Expect(podNs).NotTo(BeNil())
		Expect(podNs.ValueFrom.FieldRef.FieldPath).To(Equal("metadata.namespace"))
	})

	It("should not set durable env vars without persistence", func() {
		deleteLeases(ctx, name)
		deleteRuntime(ctx, nn)
		nn = createRuntime(ctx, "test-no-durable-env", logicv1.LogicFlowRuntimeSpec{})
		reconcileAndFetch(ctx, r, nn)

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-no-durable-env", Namespace: "default"}, &dep)).To(Succeed())
		c := mainContainer(&dep)

		Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED")).To(BeNil())
		Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME")).To(BeNil())

		deleteRuntime(ctx, types.NamespacedName{Name: "test-no-durable-env", Namespace: "default"})
	})
})
```

- [ ] **Step 8: Run all tests**

Run: `go test ./internal/controller/ -v -count=1`
Expected: All specs pass (52 existing + new lease tests)

---

### Task 3: Pod RBAC

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go`
- Test: `internal/controller/logicflowruntime_controller_test.go`

**Interfaces:**
- Consumes: `ClusterRoleDurable` constant, `logicv1.LogicFlowRuntimeKind`, `logicv1.GroupVersion`
- Produces: `reconcilePodRBAC(ctx, rt) error`, `ensureDurableClusterRole(ctx) error` — both methods on `LogicFlowRuntimeReconciler`

- [ ] **Step 1: Add RBAC markers and new imports**

Add RBAC markers after the leases marker in `logicflowruntime_controller.go`:

```go
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;delete
```

Add new imports:

```go
rbacv1 "k8s.io/api/rbac/v1"
```

- [ ] **Step 2: Add `ensureDurableClusterRole` method**

Add after `reconcileLeases`:

```go
func (r *LogicFlowRuntimeReconciler) ensureDurableClusterRole(ctx context.Context) error {
	cr := &rbacv1.ClusterRole{}
	err := r.Get(ctx, client.ObjectKey{Name: ClusterRoleDurable}, cr)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	cr = &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: ClusterRoleDurable,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "update"},
			},
		},
	}
	if err := r.Create(ctx, cr); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 3: Add `reconcilePodRBAC` method**

Add after `ensureDurableClusterRole`:

```go
func (r *LogicFlowRuntimeReconciler) reconcilePodRBAC(ctx context.Context, rt *logicv1.LogicFlowRuntime) error {
	if rt.Spec.Persistence == nil {
		return nil
	}

	if err := r.ensureDurableClusterRole(ctx); err != nil {
		return err
	}

	controller := true
	blockDeletion := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         logicv1.GroupVersion.String(),
		Kind:               logicv1.LogicFlowRuntimeKind,
		Name:               rt.Name,
		UID:                rt.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &blockDeletion,
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rt.Name,
			Namespace:       rt.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	if err := r.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rt.Name + "-durable",
			Namespace:       rt.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     ClusterRoleDurable,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      rt.Name,
				Namespace: rt.Namespace,
			},
		},
	}
	if err := r.Create(ctx, rb); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}
```

- [ ] **Step 4: Update `Reconcile` to call `reconcilePodRBAC`**

In the `Reconcile` method, add the `reconcilePodRBAC` call after `reconcileLeases`:

```go
if err := r.reconcilePodRBAC(ctx, &rt); err != nil {
	log.Error(err, "failed to reconcile pod RBAC")
}
```

- [ ] **Step 5: Write envtest tests for Pod RBAC**

Add to `internal/controller/logicflowruntime_controller_test.go`. Add new import `rbacv1 "k8s.io/api/rbac/v1"`.

Add helper:

```go
func deleteDurableClusterRole(ctx context.Context) {
	cr := &rbacv1.ClusterRole{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: ClusterRoleDurable}, cr)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
}
```

Add test contexts:

```go
Context("Pod RBAC with persistence", func() {
	const name = "test-rbac"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, persistenceSpec())
	})
	AfterEach(func() {
		deleteLeases(ctx, name)
		deleteDurableClusterRole(ctx)
		deleteRuntime(ctx, nn)
	})

	It("should create a ServiceAccount", func() {
		reconcileAndFetch(ctx, r, nn)

		var sa corev1.ServiceAccount
		Expect(k8sClient.Get(ctx, nn, &sa)).To(Succeed())
		Expect(sa.OwnerReferences).To(HaveLen(1))
		Expect(sa.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowRuntimeKind))
	})

	It("should create the shared ClusterRole", func() {
		reconcileAndFetch(ctx, r, nn)

		var cr rbacv1.ClusterRole
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: ClusterRoleDurable}, &cr)).To(Succeed())
		Expect(cr.Rules).To(HaveLen(1))
		Expect(cr.Rules[0].APIGroups).To(Equal([]string{"coordination.k8s.io"}))
		Expect(cr.Rules[0].Resources).To(Equal([]string{"leases"}))
		Expect(cr.Rules[0].Verbs).To(ConsistOf("get", "list", "watch", "update"))
		Expect(cr.OwnerReferences).To(BeEmpty())
	})

	It("should create a RoleBinding", func() {
		reconcileAndFetch(ctx, r, nn)

		var rb rbacv1.RoleBinding
		rbNN := types.NamespacedName{Name: name + "-durable", Namespace: "default"}
		Expect(k8sClient.Get(ctx, rbNN, &rb)).To(Succeed())
		Expect(rb.RoleRef.Name).To(Equal(ClusterRoleDurable))
		Expect(rb.RoleRef.Kind).To(Equal("ClusterRole"))
		Expect(rb.Subjects).To(HaveLen(1))
		Expect(rb.Subjects[0].Name).To(Equal(name))
		Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
		Expect(rb.OwnerReferences).To(HaveLen(1))
		Expect(rb.OwnerReferences[0].Kind).To(Equal(logicv1.LogicFlowRuntimeKind))
	})

	It("should set serviceAccountName on the Deployment", func() {
		reconcileAndFetch(ctx, r, nn)

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(name))
	})
})

Context("No RBAC without persistence", func() {
	const name = "test-no-rbac"
	var nn types.NamespacedName
	var r *LogicFlowRuntimeReconciler

	BeforeEach(func() {
		r = newReconciler()
		nn = createRuntime(ctx, name, logicv1.LogicFlowRuntimeSpec{})
	})
	AfterEach(func() {
		deleteRuntime(ctx, nn)
	})

	It("should not create ServiceAccount or RoleBinding", func() {
		reconcileAndFetch(ctx, r, nn)

		var sa corev1.ServiceAccount
		err := k8sClient.Get(ctx, nn, &sa)
		Expect(errors.IsNotFound(err)).To(BeTrue())

		var rb rbacv1.RoleBinding
		rbNN := types.NamespacedName{Name: name + "-durable", Namespace: "default"}
		err = k8sClient.Get(ctx, rbNN, &rb)
		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("should not set serviceAccountName on the Deployment", func() {
		reconcileAndFetch(ctx, r, nn)

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, nn, &dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(BeEmpty())
	})
})
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/controller/ -v -count=1`
Expected: All specs pass

---

### Task 4: Integration tests

**Files:**
- Modify: `internal/controller/integration_test.go`

**Interfaces:**
- Consumes: `persistenceSpec()`, `listLeases()`, `deleteLeases()`, `deleteDurableClusterRole()` from Task 2-3 test helpers; `reconcileAndFetch`, `reconcileDefAndFetch`, `createRuntime`, `createDefinition`, `deleteRuntime`, `deleteDefinition` from existing test helpers

- [ ] **Step 1: Add durable integration test context**

Add to `internal/controller/integration_test.go`, inside the existing `Describe("Cross-controller integration")` block. Add new imports `coordinationv1 "k8s.io/api/coordination/v1"` and `rbacv1 "k8s.io/api/rbac/v1"`:

```go
Context("Durable leases with ConfigMap integration", func() {
	const rtName = "integ-durable-rt"
	const defName = "integ-durable-def"
	var rtNN, defNN types.NamespacedName
	var rtRec *LogicFlowRuntimeReconciler
	var defRec *LogicFlowDefinitionReconciler

	BeforeEach(func() {
		rtRec = newReconciler()
		defRec = newDefReconciler()
		rtNN = createRuntime(ctx, rtName, persistenceSpec())
	})
	AfterEach(func() {
		deleteDefinition(ctx, defNN)
		deleteLeases(ctx, rtName)
		deleteDurableClusterRole(ctx)
		deleteRuntime(ctx, rtNN)
	})

	It("should create leases, mount ConfigMaps, and set durable env vars", func() {
		// Step 1: Reconcile Runtime — leases created, durable env vars set
		rt := reconcileAndFetch(ctx, rtRec, rtNN)
		leases := listLeases(ctx, rtName)
		Expect(leases).To(HaveLen(1))
		Expect(rt.Status.LeaseReplicas).To(Equal(int32(1)))

		// Verify durable env vars
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
		c := mainContainer(&dep)
		Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_LEASE_LEADER_ENABLED")).NotTo(BeNil())
		Expect(findEnvVar(c.Env, "QUARKUS_FLOW_DURABLE_KUBE_POOL_NAME").Value).To(Equal(rtName))

		// Verify RBAC
		var sa corev1.ServiceAccount
		Expect(k8sClient.Get(ctx, rtNN, &sa)).To(Succeed())
		Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(rtName))

		// Step 2: Add a Definition — ConfigMap mounted alongside leases
		defNN = createDefinition(ctx, defName, rtName, validFlowJSON("durable-flow", "1.0.0", "ns"))
		def := reconcileDefAndFetch(ctx, defRec, defNN)
		cmName := def.Status.ConfigMapRef.Name

		rt = reconcileAndFetch(ctx, rtRec, rtNN)
		Expect(rt.Status.Definitions).To(HaveLen(1))
		Expect(findVolume(&dep, cmName)).To(BeNil()) // need fresh dep
		Expect(k8sClient.Get(ctx, rtNN, &dep)).To(Succeed())
		Expect(findVolume(&dep, cmName)).NotTo(BeNil())

		// Leases still present
		leases = listLeases(ctx, rtName)
		Expect(leases).To(HaveLen(1))

		// Step 3: Scale up — new leases created
		Expect(k8sClient.Get(ctx, rtNN, &rt)).To(Succeed())
		replicas := int32(3)
		rt.Spec.Replicas = &replicas
		Expect(k8sClient.Update(ctx, &rt)).To(Succeed())

		rt2 := reconcileAndFetch(ctx, rtRec, rtNN)
		leases = listLeases(ctx, rtName)
		Expect(leases).To(HaveLen(3))
		Expect(rt2.Status.LeaseReplicas).To(Equal(int32(3)))

		// ConfigMap still mounted
		Expect(rt2.Status.Definitions).To(HaveLen(1))
	})
})
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./internal/controller/ -v -count=1`
Expected: All specs pass

- [ ] **Step 3: Regenerate RBAC manifests**

Run: `make manifests`
Expected: `config/rbac/role.yaml` updated with new coordination.k8s.io, serviceaccounts, clusterroles, and rolebindings permissions
