# ClusterLogicPlatform Runtime Defaults Design

**Status:** Proposed
**Target:** ClusterLogicPlatform / LogicFlowRuntime v1 API

---

## Context

Every LogicFlowRuntime repeats the same deployment settings (resources,
environment variables, probe timings). ClusterLogicPlatform already owns runtime-wide
settings (persistence), so it should also provide default deployment
configuration that runtimes inherit.

## Scope

- `ClusterLogicPlatform.spec.runtime` accepts a **restricted mirror** of
  `ApplicationSpec` (`ApplicationDefaultsSpec`), inlined next to `persistence`.
- Values set there act as defaults for `LogicFlowRuntime.spec.ApplicationSpec`.
- The merge happens at reconcile time, not at admission.

Non-goals: defaults for other platform components (Data Index, Gateway, Vector)
and per-namespace defaults.

## Precedence

Highest to lowest:

1. LogicFlowRuntime value
2. ClusterLogicPlatform `spec.runtime` value
3. Operator fallback (only fills a field that is still unset)

A value counts as "set" when it is non-zero: pointer non-nil, string non-empty,
slice or map non-empty.

## API: mirror types

The platform does not inline the full `ApplicationSpec`. It inlines a smaller
type with the same JSON paths and the same Go types for every field it keeps.
Fields the platform must not control are absent, so the CRD schema rejects
them. Allowing more later means adding a field to the mirror type, with no
refactoring.

```go
// ApplicationDefaultsSpec is the subset of ApplicationSpec allowed as platform defaults.
// JSON paths and field types must match ApplicationSpec; adding a field means adding it here.
type ApplicationDefaultsSpec struct {
	ImagePullPolicy corev1.PullPolicy           `json:"imagePullPolicy,omitempty"` // Enum=Always;Never;IfNotPresent
	Replicas        *int32                      `json:"replicas,omitempty"`        // Minimum=0
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
	PodTemplate     PodTemplateDefaultsSpec     `json:"podTemplate,omitempty"`
	Container       ContainerDefaultsSpec       `json:"container,omitempty"`
}

// PodTemplateDefaultsSpec mirrors PodTemplateSpec for the allowed pod-level defaults.
type PodTemplateDefaultsSpec struct {
	Metadata        *PodTemplateMetadata  `json:"metadata,omitempty"`
	Container       ContainerDefaultsSpec `json:"container,omitempty"`
	PodSpecDefaults `json:",inline"` // PodTemplateSpec inlines PodSpec
}

// PodSpecDefaults mirrors PodSpec. It is empty in v1; allowed pod-level fields are added here.
type PodSpecDefaults struct{}

// ContainerDefaultsSpec mirrors ContainerSpec for the allowed container-level defaults.
type ContainerDefaultsSpec struct {
	ImagePullPolicy corev1.PullPolicy           `json:"imagePullPolicy,omitempty"` // Enum=Always;Never;IfNotPresent
	Resources       corev1.ResourceRequirements `json:"resources,omitempty"`
	Env             []EnvVarDefault             `json:"env,omitempty"`
	LivenessProbe   *ProbeTuning                `json:"livenessProbe,omitempty"`
	ReadinessProbe  *ProbeTuning                `json:"readinessProbe,omitempty"`
	StartupProbe    *ProbeTuning                `json:"startupProbe,omitempty"` // durable runtimes only
}

// EnvVarDefault is a literal name/value pair, a field subset of corev1.EnvVar.
type EnvVarDefault struct {
	Name  string `json:"name"`            // MinLength=1
	Value string `json:"value,omitempty"`
}

// ProbeTuning holds probe timings, a field subset of corev1.Probe.
type ProbeTuning struct {
	InitialDelaySeconds *int32 `json:"initialDelaySeconds,omitempty"` // Minimum=0
	PeriodSeconds       *int32 `json:"periodSeconds,omitempty"`       // Minimum=1
	TimeoutSeconds      *int32 `json:"timeoutSeconds,omitempty"`      // Minimum=1
	FailureThreshold    *int32 `json:"failureThreshold,omitempty"`    // Minimum=1
}
```

Wiring:

```go
type RuntimePlatformSpec struct {
	// Defaults are inherited by LogicFlowRuntimes; the runtime value wins.
	ApplicationDefaultsSpec `json:",inline"`

	Persistence *RuntimePersistenceSpec `json:"persistence,omitempty"`
}
```

Inlining keeps the YAML paths identical to `ApplicationSpec`, for example
`spec.runtime.container.env`. A later switch of a field to the full type is a
superset, so existing platform YAML stays valid.

Rules for the types:

- No `+kubebuilder:default` markers. The API server must never fill in a value
  on the platform. Copy `Enum` and `Minimum` markers, never defaults.
- Doc comments stay at one or two lines, per project conventions.

### Included in v1

| Level | Fields |
|---|---|
| `ApplicationSpec` | `imagePullPolicy`, `replicas`, `resources` |
| `container`, `podTemplate.container` | `imagePullPolicy`, `resources`, `env` (literal only), `livenessProbe`, `readinessProbe`, `startupProbe` (timings only) |
| `podTemplate` | `metadata.labels`, `metadata.annotations` |

### Not supported as defaults

| Field | Reason |
|---|---|
| `image` (top-level and `container`) | The minimal or standard runner image depends on each runtime's persistence mode, so the operator keeps choosing it (`DefaultRunnerImage`) |
| `command`, `args`, `workingDir` | Change what runs; depend on the runner image |
| `envFrom`, `env[].valueFrom` | Reference Secrets/ConfigMaps that may not exist in a tenant namespace |
| `volumeMounts`, `volumes` | Must match each other; the operator appends its own ConfigMap volumes and mounts |
| `securityContext` (container, pod) | Replaces the operator's restricted defaults whole |
| `lifecycle` | Workload-specific |
| Probe handlers (`httpGet`, `exec`, `tcpSocket`) | A handler-less probe is invalid; the operator owns the Quarkus health paths |
| `serviceAccountName` | The operator forces `rt.Name` for durable runtimes |
| `initContainers`, sidecar `containers` | Arbitrary code in tenant pods; see Future work |
| `imagePullSecrets` | Secrets must exist in every tenant namespace |
| `hostAliases`, `dnsConfig`, `dnsPolicy`, `restartPolicy` | Change networking and restart behavior; the last two carry CRD defaults a platform value could not override |
| `terminationGracePeriodSeconds` | Workload-specific |
| `nodeSelector`, `tolerations`, `affinity` (including `nodeAffinity`), `priorityClassName` | A bad value leaves every inheriting runtime's pods `Pending` (or rejected, for a missing PriorityClass), and the runtime's status does not point to the platform as the source; see Future work |
| `podAffinity`, `podAntiAffinity`, `topologySpreadConstraints` | Their `labelSelector` cannot match each runtime's own pod labels, and they can reference other tenants' pods |
| `schedulerName` | No platform-wide use case identified |
| `podDisruptionBudget` | Not implemented by the controller today; a default would have no effect |
| `podTemplate.replicas` | Duplicates top-level `replicas`; a runtime's own value still wins |

## Merge Semantics

Platform values are the lowest-priority user layer. Anything set in the
LogicFlowRuntime wins, whichever tier it is in.

The two specs are NOT merged field by field before the tiers are resolved.
Merging first would let a platform `container.imagePullPolicy` beat a runtime
top-level `imagePullPolicy`, because `container` outranks the top level. Instead:

1. **Flatten each spec independently.** Apply the existing rule
   `podTemplate.container > container > top-level` (see `resolveContainerAC`)
   to the platform defaults and to the runtime spec separately. Each yields one
   flat container description. Replicas collapse the same way
   (`podTemplate.replicas > replicas`).
2. **Overlay.** For every field the flattened runtime container sets, the
   runtime value wins. Otherwise the platform value is used.
3. **Operator fallbacks.** The existing `ContainerOption`s run on the effective
   container and only fill what is still unset (see below).

Example: platform sets `container.imagePullPolicy: Always`, runtime sets
top-level `imagePullPolicy: Never`. Flattening gives Always and Never. The
overlay picks Never.

### Depth

Merging is one level deep, matching the current controller
(`applyContainerFields`). Composed fields (`resources`) are replaced whole,
never merged.

Exceptions:

- **`env` merges by name.** The platform variables apply unless the runtime
  defines the same name, in which case the runtime wins. Replacing the list
  would silently drop platform variables as soon as a tenant adds one.
- **`podTemplate.metadata` labels and annotations merge as maps**, platform
  first and runtime second, so the runtime wins on a key conflict. This follows
  `MergeMaps`, where later maps win.

### Pod-level fields

v1 allows no `PodSpec` fields as defaults (see "Not supported as defaults").
Only `podTemplate.metadata` and `podTemplate.container` are mirrored.

### Probes

Probe tuning is not converted into `corev1.Probe`. A timing-only probe has no
handler; `applyContainerFields` would apply it, `DefaultQuarkusProbes` would see
a non-nil probe and do nothing, and the API server would reject the pod.

Instead the tuning is passed to the operator fallbacks:

- `DefaultQuarkusProbes(tuning)` and `DurableStartupProbe(tuning)` build the
  operator's default probe, keep its `httpGet`, and replace only the timings the
  platform set.
- They still act only when the probe is `nil`. A probe set in the runtime wins
  whole, and platform timings are not applied to it.
- The startup probe is created for durable runtimes only, so a platform
  `startupProbe` has no effect on ephemeral runtimes.

Precedence: runtime probe, then platform timings, then operator defaults.

### Operator fallbacks and always-applied settings

Fill only when unset, so a platform value counts as set:

- `DefaultRunnerImage` (always applies: the platform never sets an image, and
  the minimal or standard variant depends on the runtime's persistence mode)
- `DefaultQuarkusProbes`, `DurableStartupProbe`
- `imagePullPolicy` (see API Changes)
- replicas: 1 when nothing is set (`effectiveReplicas`)

Always applied on top of the effective container (not fallbacks):

- Env vars from `WithQuarkusPersistenceEnvVars`, `WithSecurityEnvVars`,
  `WithStructuredLoggingEnvVars`, `WithQuartzSchedulerEnvVars` and others are appended.
- `WithFlowSourcePath` and `WithDurableEnvVars` remove user-supplied values for
  their protected variables, so platform `env` gets the same protection.
- ConfigMap volumes and mounts for LogicFlowDefinitions are appended after the
  user volumes (`applyDeployment`, `WithFlowVolumeMounts`).

## Conversion

```go
// ToApplicationSpec converts the defaults into an ApplicationSpec via JSON,
// like convertTo does for apply configurations. Probe tuning is excluded.
func (d *ApplicationDefaultsSpec) ToApplicationSpec() (ApplicationSpec, error)

// ProbeDefaults returns the probe tunings, flattened (podTemplate.container over container).
func (d *ApplicationDefaultsSpec) ProbeDefaults() (live, ready, startup *ProbeTuning)
```

`ToApplicationSpec` feeds the flatten-and-overlay code. `ProbeDefaults` feeds the
probe fallbacks. Because the JSON paths match, no per-field copy code exists.

## Validation

Platform values are suggestions. Admission validation of a LogicFlowRuntime
looks only at the runtime's own spec (for example `validateRunnerImage` checks
`spec.image`), and platform defaults never have to satisfy a runtime's required
fields.

Platform webhook (`ClusterLogicPlatform`):

- `env` names must be valid environment variable names and unique.
- `env` names managed by the operator are rejected (`QUARKUS_DATASOURCE_*`,
  `QUARKUS_QUARTZ_*`, and the names set by `WithFlowSourcePath`,
  `WithDurableEnvVars`, `WithSecurityEnvVars` and `WithStructuredLoggingEnvVars`).
  Derive the list from the helpers so it cannot drift.
- Startup probe: `periodSeconds × failureThreshold ≥ 60`, using the operator
  default for an unset value, so pods are not restarted before they acquire a
  lease. Confirm the floor with the owner of the lease logic.

## Implementation

Add a pure function in `internal/controller` that takes the runtime
`ApplicationSpec` and the platform defaults and returns the effective
`ApplicationSpec` (flatten, then overlay), plus the probe tunings. Existing
functions keep their signatures and receive its result:

- `applyDeployment` (`ToDeploymentSpec`, `durableDeploymentStrategy`)
- `reconcileLeases` and `updateStatusLeases` (`effectiveReplicas`)
- `WithQuartzSchedulerEnvVars` (`effectiveReplicas`)

All consumers must use the same effective spec so they agree on replicas.

Platform lookup reuses `resolvePlatform`. Platform changes already re-enqueue
runtimes through `mapPlatformToRuntimes`, so no new watch is needed.

## API Changes

- `RuntimePlatformSpec` inlines `ApplicationDefaultsSpec`.
- Remove `+kubebuilder:default=IfNotPresent` from `ApplicationSpec.ImagePullPolicy`
  and apply it in code after the overlay. Otherwise the API server fills it in
  on every runtime and a platform value can never win.

## Observability

Record the effective replicas in an event when platform defaults change a
runtime's Deployment, and list the names (not values) of the platform fields
and `env` variables applied. Full effective-spec reporting in status is deferred.

## Alternatives Considered

| Option | Why rejected |
|---|---|
| Inline the full `ApplicationSpec` and ignore unsupported fields in the controller | Advertises fields that do nothing; a stored value could start applying after an upgrade |
| Inline the full `ApplicationSpec` and reject fields in the webhook | Works, but fails open unless written as an allow-list, and shows the full schema in `kubectl explain` |
| Mutating webhook copies defaults at creation | Loses live propagation; later platform changes do not affect existing runtimes |
| Separate `platformDefaults` map | Duplicates the ApplicationSpec schema |
| Recursive deep merge | Needs list-merge rules (volumes, containers); deferred until users need it |
| Merge both specs into one `ApplicationSpec`, then resolve the tiers | A platform `container.*` value would override a runtime top-level value, because `container` outranks the top level |
| Six-layer resolution inside `resolveContainerAC` | Changes the signatures of several functions and every call site; a consumer that forgets the platform argument disagrees with the others |

## Risks

- A platform default change that alters the effective pod template (`env`,
  `resources`, `imagePullPolicy`, probe timings, pod labels or annotations) rolls
  out every inheriting runtime at once. A change that does not alter a runtime's
  effective pod template (for example, the runtime already overrides the field)
  causes no restart. Single-replica runtimes use `Recreate`
  (`durableDeploymentStrategy`), so they incur downtime during the rollout.
  Changing `replicas` scales without restarting pods.
- Platform `env` values are visible to anyone who can read the platform CR. Do
  not put credentials there.
- Removing the `imagePullPolicy` CRD default changes the stored value on existing runtimes.
- A runtime that sets `resources` replaces the platform's `resources` whole, silently.
- Operator-set env vars that only append (`WithQuarkusPersistenceEnvVars`,
  `WithQuartzSchedulerEnvVars`, and similar) can duplicate a platform name; the
  webhook's reserved-name rule prevents this.

## Files Modified

- `api/v1/platform_types.go`
- `api/v1/deployment_types.go`
- `api/v1/clusterlogicplatform_webhook.go`
- `internal/controller/application_objects.go`
- `internal/controller/logicflowruntime_controller.go`
- `internal/controller/quarkus_config.go`
- generated: `zz_generated.deepcopy.go`, CRD manifests

## Testing

Drift test: walk each defaults type and its `ApplicationSpec` counterpart by
JSON path. For every path assert it exists in the full type with the same Go
type. For `EnvVarDefault` and `ProbeTuning`, assert each field exists in
`corev1.EnvVar` or `corev1.Probe` with the same name, JSON tag and type.

Unit (flatten and overlay): runtime only, platform only, both set, nested overlap.

Required cases:

- Cross-tier: platform `container.imagePullPolicy`, runtime top-level `imagePullPolicy`; the runtime wins.
- The runner image is never taken from the platform: `DefaultRunnerImage` picks
  the minimal or standard variant from each runtime's persistence mode.
- `env` merge by name: platform `[A, B]`, runtime `[B', C]` gives `[A, B', C]`.
- Platform `env` using an operator-managed name is rejected by the webhook.
- Probe tuning: platform timings applied to the operator default probe; a runtime probe wins whole.
- Startup probe below the floor is rejected.
- `resources` with only `claims` set (counts as set under a generic zero check,
  unlike the current `Requests`/`Limits` check).
- Platform and runtime both set `podTemplate.metadata.labels`; the maps merge.
- Platform sets `imagePullPolicy`, runtime does not; the platform value is used.
- Replicas agree across `ToDeploymentSpec`, lease reconciliation and Quartz env vars.

Envtest: platform default applied to the Deployment; runtime override wins;
platform change triggers a rollout.

## Future work

- `podDisruptionBudget`: the field exists in the API but no controller creates a
  PodDisruptionBudget. Implement that first. Prefer `maxUnavailable` or
  percentages, because a fixed `minAvailable` can block drains on one-replica runtimes.
- `initContainers`: append platform containers before the runtime's, with a
  rule for name collisions. Use a constrained type (`name`, `image`, `command`,
  `args`, literal `env`, `resources`) with an operator-applied restricted
  security context, because no restricted default is applied to init containers today.
- Runtime-level probe tuning, so tenants can adjust timings without replacing a probe.
- `podAntiAffinity` and `topologySpreadConstraints`, if the selector problem is solved.
- Scheduling defaults (`nodeSelector`, `tolerations`, `affinity.nodeAffinity`,
  `priorityClassName`). They were left out of v1 because a bad value leaves every
  inheriting runtime's pods `Pending` (or rejected, for a missing PriorityClass),
  and the runtime's status does not point to the platform as the source. Add them
  only together with a runtime status condition that names the platform-supplied
  fields and surfaces the scheduler's reason, and prefer `preferred` over
  `required` node affinity. Adding each one is a single field in `PodSpecDefaults`.
- Runner image registry mirror or version pinning: add a separate field with one
  image per variant (minimal and standard), chosen by the persistence mode. Do
  not reuse a mirrored `image` field, and keep the runtime's own `spec.image` as
  the final, validated override.

## Verified

- `PodSpecApplyConfiguration.WithVolumes` appends rather than replaces. The
  operator keeps this behavior: user `podTemplate.volumes` are added first, then
  the operator appends its ConfigMap volumes.

## Unverified

- Which of two env entries with the same name Kubernetes uses is not confirmed
  for this controller; the reserved-name rule avoids relying on it.

## Open Questions

1. Should a later version merge composed fields (`resources`) recursively?
