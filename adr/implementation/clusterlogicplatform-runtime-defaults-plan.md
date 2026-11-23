# ClusterLogicPlatform Runtime Defaults Implementation Plan

**Design:** [clusterlogicplatform-runtime-defaults.md](clusterlogicplatform-runtime-defaults.md)

**Goal:** Let `ClusterLogicPlatform.spec.runtime` provide defaults for the `ApplicationSpec` of every `LogicFlowRuntime` that uses the platform.

**Approach:** A pure merge function in `internal/controller` produces an effective `ApplicationSpec` (flatten each spec, overlay with the runtime winning, then apply operator fallbacks). Existing functions keep their signatures and receive the result.

## Global Constraints

- Precedence: runtime value, then platform value, then operator fallback.
- A platform value is never required for a runtime to be valid, and never rejects a runtime at admission.
- `image` and all scheduling fields are not supported as defaults in v1.
- No license headers on new files; preserve existing headers on modified files.
- Unit tests: standard `testing` + `gomega.NewWithT(t)`. Envtest: Ginkgo v2 + Gomega.
- No commits without `APPROVED: commit`.
- Run `make test` after every task; each task must leave the build green.

## Owners

- Tasks 1, 2, 3, 4, 5, 6, 7, 8: Bob implements. The user reviews.
- Tests in each task: Bob writes on request.

---

### Task 1: API types

**Files:**
- Modify: `api/v1/platform_types.go`
- Modify: `api/v1/deployment_types.go`
- Generated: `api/v1/zz_generated.deepcopy.go`, CRD manifests

- [ ] Add `ApplicationDefaultsSpec`, `PodTemplateDefaultsSpec`, `PodSpecDefaults`, `ContainerDefaultsSpec`, `EnvVarDefault`, `ProbeTuning` as described in the design (API: mirror types).
- [ ] Inline `ApplicationDefaultsSpec` in `RuntimePlatformSpec`.
- [ ] Remove `+kubebuilder:default=IfNotPresent` from `ApplicationSpec.ImagePullPolicy`.
- [ ] Run `make generate manifests`.
- [ ] Run `make test`; fix any test that relied on the CRD default.

### Task 2: `imagePullPolicy` fallback in the controller

Isolated so the behavior change from removing the CRD default can be reviewed on its own.

**Files:**
- Modify: `internal/controller/application_objects.go`
- Test: `internal/controller/application_objects_test.go`

- [ ] Apply `IfNotPresent` in code when no tier sets `imagePullPolicy`.
- [ ] Test: runtime with no policy gets `IfNotPresent`; explicit policy is kept.
- [ ] Run `make test`.

### Task 3: Merge function

**Files:**
- Modify: `internal/controller/application_objects.go` (or a new file in the same package)
- Test: `internal/controller/application_objects_test.go`

- [ ] Implement the pure function: runtime `ApplicationSpec` + platform defaults in, effective `ApplicationSpec` and probe tunings out.
- [ ] Flatten each spec independently across its three tiers, then overlay (runtime wins).
- [ ] Merge `env` by name; merge pod `labels` and `annotations` as maps; replace composed fields (`resources`) whole.
- [ ] Tests (see design, Testing): cross-tier precedence, `env` merge, `resources` with only `claims`, label map merge, platform `imagePullPolicy` used when the runtime has none, runner image never taken from the platform.
- [ ] Run `make test`.

### Task 4: Wire the effective spec into the controller

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go`
- Modify: `internal/controller/quarkus_config.go`

- [ ] Resolve the platform with `resolvePlatform` and compute the effective spec once per reconcile.
- [ ] Pass it to `applyDeployment` (`ToDeploymentSpec`, `durableDeploymentStrategy`), `reconcileLeases`, `updateStatusLeases` and `WithQuartzSchedulerEnvVars`.
- [ ] Test: a platform `replicas` value yields the same replica count in the Deployment, the leases and the Quartz env vars.
- [ ] Run `make test`.

### Task 5: Probe tuning

**Files:**
- Modify: `internal/controller/application_objects.go`
- Modify: `internal/controller/quarkus_config.go`

- [ ] Apply platform timings onto the operator default probes (`DefaultQuarkusProbes`, `DurableStartupProbe`).
- [ ] A probe the runtime defines wins whole; do not convert timing-only probes to `corev1.Probe`.
- [ ] Tests: timings applied to the default probe; runtime probe wins.
- [ ] Run `make test`.

### Task 6: Platform webhook validation

**Files:**
- Modify: `api/v1/clusterlogicplatform_webhook.go`
- Test: `api/v1/clusterlogicplatform_webhook_test.go`

- [ ] Reject platform `env` names reserved by the operator.
- [ ] Reject a startup probe below the 60s floor.
- [ ] Tests: each rule accepts a valid value and rejects an invalid one.
- [ ] Run `make test`.

### Task 7: Observability and envtest

**Files:**
- Modify: `internal/controller/logicflowruntime_controller.go`
- Test: `internal/controller/logicflowruntime_controller_test.go`

- [ ] Emit an event listing the names (not values) of platform fields and `env` variables applied, including the effective replicas.
- [ ] Envtest: platform default reaches the Deployment; runtime override wins; a platform change triggers a rollout.
- [ ] Run `make test`.

### Task 8: Close out

- [ ] Verify the duplicate `env` name behavior from the design's Unverified section and move it to Verified.
- [ ] Update the Status line in the design note.
- [ ] Optionally add a row to the status table in `adr/001-logic-operator-v2-architecture.md`.
- [ ] Stage changes and suggest a commit message; wait for `APPROVED: commit`.
