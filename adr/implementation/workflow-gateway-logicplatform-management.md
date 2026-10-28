# Workflow Gateway Managed by LogicPlatform

**Status:** Proposed for implementation

**Date:** 2026-10-26

**Issue:** [logic-operator #42 — Operator deploys workflow gateway](https://github.com/kubesmarts/logic-operator/issues/42)

**Related:** [Workflow Gateway — Lease-Based Instance Routing](workflow-gateway-instance-routing.md), [Workflow Gateway CRD Discovery](workflow-gateway-crd-discovery.md), [LogicPlatform Vector Implementation](logicplatform-vector-implementation.md)

## Summary

Make the workflow-gateway an optional component managed by a `LogicPlatform`. When `spec.workflowGateway` is present, the LogicPlatform controller manages the gateway Deployment and ClusterIP Service, its ServiceAccount and cluster-scoped read permissions, and an optional public Kubernetes Ingress or OpenShift Route. The gateway's public URL and child-resource references are reported in `LogicPlatform.status.workflowGateway`.

This issue also makes `LogicFlowService` optional for gateway execution: the gateway prefers its URL when present and falls back to the runtime Kubernetes Service derived from `LogicFlowDefinition.spec.runtimeRef`. It must not add an ingress URL to `LogicFlowRuntime` or turn sticky sessions back on. The gateway intentionally uses different paths for execution and following up on a known instance, as described below.

## Context

The current implementation has these relevant properties:

- `LogicPlatform` is namespaced and limited to one resource per namespace. Its controller already reconciles Data Index Deployments, Services and optional Ingress/Route resources, plus an optional Vector DaemonSet.
- The gateway is a separate Go module at `workflow-gateway/`, built and published from this repository. The root Makefile defines `GATEWAY_IMG` as `kubesmarts.org/workflow-gateway:v$(GATEWAY_VERSION)`, with the version configured independently in `versions.mk`.
- `workflow-gateway/cmd/main.go` listens on the user API at `:8080` and the management API at `:8081`. Management endpoints are `GET /health` and `GET /ready`. `/ready` remains 503 until the controller-runtime cache and initial catalog recompute are ready. The process reads `POD_NAMESPACE` to select the LogicPlatform whose Data Index endpoint it watches; if the variable is missing, it falls back to `default`.
- The gateway watches `LogicFlowDefinitions` and optional `LogicFlowServices` to build its catalog. Its `PlatformWatcher` reads `LogicPlatform` resources and selects the Data Index endpoint in `POD_NAMESPACE`.
- For execution, a matching `LogicFlowService.status.url` takes precedence and preserves that resource's ingress routing and traffic policy. When no matching LogicFlowService route exists, the gateway derives an internal endpoint from `LogicFlowDefinition.spec.runtimeRef.name` and calls that runtime's Kubernetes Service on port 80. This makes LogicFlowService optional for gateway-routed workflows.
- For an asynchronous execution, the gateway records the returned `workflowApplicationId`; follow-up operations resolve that known instance to its lease holder and send the request directly to the owning pod on the runner port. Follow-up routing does not use either ingress URL or the round-robin runtime Service because the request must reach the pod that owns the instance's in-memory state.
- `config/rbac/workflow_gateway_clusterrole.yaml` grants read access to LogicFlowServices, LogicFlowDefinitions, LogicPlatforms and Leases. LogicFlowServices remain optional at runtime, but the gateway watches them so their URLs can override the runtime-Service fallback. `config/default/kustomization.yaml` prefixes the source ClusterRole name, producing the installed name `logic-operator-workflow-gateway`, which matches the `ClusterRoleWorkflowGateway` Go constant.
- `LogicFlowRuntime.status` has no external URL field. A public URL for the gateway itself belongs to `LogicPlatform.status.workflowGateway.url`.

### Correct the stale Issue #42 acceptance criteria

Issue #42 was written against an earlier routing design. Apply the following interpretation when implementing it:

| Issue #42 wording | Decision for this implementation |
|---|---|
| Deploy the gateway Deployment | Keep. Deploy when `spec.workflowGateway` is present. |
| Create a Service and Ingress/Route | Keep with a qualification: always create the ClusterIP Service; create Ingress/Route only when `spec.workflowGateway.ingress.enabled` is true. |
| Populate an ingress URL on LogicFlowRuntime | Replace. Do not change LogicFlowRuntime. The gateway's public URL is reported on LogicPlatform status; when a LogicFlowService is configured, its external URL remains on that resource. |
| Ensure runtime Ingresses have sticky-session annotations | Drop. Instance follow-up requests are routed directly to the owning pod, so they do not depend on ingress affinity. Issue #42 does not add or restore sticky-session annotations. |

### Preserve the execution and instance-follow-up paths

Issue #42 manages the gateway's lifecycle and front door and makes `LogicFlowService` optional for gateway execution. The catalog prefers `LogicFlowService.status.url` when a matching Service exists; otherwise, it routes to the Kubernetes Service named by the Definition's `runtimeRef`. Instance follow-up requests use the cached `instanceId → workflowApplicationId` mapping, resolve the current lease holder and reach that pod directly on the runner port. This keeps the follow-up path pinned to the current owner of that specific instance.

No runtime sticky-session annotation is needed for this model: follow-up traffic does not depend on the ingress selecting the same pod. Keep the `workflow-gateway-instance-routing.md` rationale when describing why follow-up requests use the lease holder.

## Goals

1. Allow a LogicPlatform author to opt into an operator-managed workflow-gateway.
2. Provide an internal stable Service for the gateway and an optional external entry point.
3. Give the gateway only the read permissions its discovery and routing code requires.
4. Report gateway availability, replica counts, resource references and external URL through LogicPlatform status.
5. Remove gateway-specific resources and permissions when the component is disabled or the LogicPlatform is deleted.
6. Keep defaults consistent with the repository's gateway image build and release flow.
7. Support a user-managed HPA against the gateway Deployment while preserving the in-memory instance cache's single-replica safety limit.

## Non-goals

- Change `LogicFlowRuntime` or `LogicFlowService` APIs or their networking behavior.
- Add or restore sticky-session annotations.
- Change workflow-gateway REST behavior, authentication, timeouts, or its runtime forwarding algorithm.
- Expose the management port through the Service or public Ingress/Route.
- Add Gateway API `HTTPRoute` support for the gateway front door in this issue. The front door uses Kubernetes Ingress or OpenShift Route, matching the existing LogicPlatform Data Index pattern.
- Add an operator-managed HPA or PodDisruptionBudget. Users may create an HPA targeting the generated gateway Deployment.

## Decisions

### 1. API shape and enablement

Add an optional pointer field to `LogicPlatformSpec`:

```yaml
spec:
  workflowGateway:
    application:
      image: kubesmarts.org/workflow-gateway:v0.0.1
      replicas: 1
      resources:
        requests:
          cpu: 100m
          memory: 128Mi
    ingress:
      enabled: true
      host: workflows.example.com
      tls:
        enabled: true
        secretRef:
          name: workflows-tls
```

Use the existing `ApplicationSpec` for the `application` field so image, replica count, resources and supported pod/container customization follow the same model as Data Index and runtime workloads. Use the existing `DataIndexIngressSpec` for the optional `ingress` field so host, class, annotations and TLS are configured consistently.

The presence of `spec.workflowGateway` enables the Deployment, Service, ServiceAccount and gateway permissions. If the field is absent, the gateway is disabled and its resources are removed. There is no second `enabled` boolean on `WorkflowGatewaySpec`; this avoids a present-but-disabled configuration whose intended lifecycle is unclear. Ingress is independently opt-in through `spec.workflowGateway.ingress.enabled`.

When `spec.workflowGateway` is present and values are omitted, default the gateway image to the version configured for this operator build and run one replica. The default must stay aligned with `GATEWAY_IMG` / `GATEWAY_VERSION` from `versions.mk`; do not silently ship an unrelated hard-coded tag. Keep resource requests/limits user-configurable and do not invent different resource defaults in this issue without measurements.

The gateway currently keeps the async instance-to-workflow-application mapping in process memory. Multiple replicas do not share this mapping, so an instance follow-up request sent to a different replica may fail to resolve its owner. Keep the default at one replica, and document that a user-managed HPA must set `maxReplicas: 1` until a separate change adds a shared PostgreSQL-backed lookup. Do not add an HPA CRD field or have the operator create an HPA in this issue.

Run gateway image and ingress validation whenever `spec.workflowGateway` is configured, even when `spec.dataIndex.enabled` is false. The current LogicPlatform validator returns early when Data Index is disabled, so restructure that flow rather than accidentally skipping gateway validation. Use error messages naming the `spec.workflowGateway` path. The gateway can be deployed without Data Index; its Data Index-backed query endpoints will remain unavailable as reported by the gateway, while workflow execution routing remains independently configured.

### 2. Resource naming and ownership

Use deterministic names derived from the LogicPlatform name, with the `-workflow-gateway` suffix for namespaced gateway resources:

- Deployment, Service, ServiceAccount and Ingress/Route: `<platform-name>-workflow-gateway`
- ClusterRoleBinding: `<platform-namespace>-<platform-name>-workflow-gateway`

Namespaced resources carry the standard LogicPlatform child labels and a controller owner reference. The ClusterRoleBinding is cluster-scoped and must **not** use a namespaced LogicPlatform owner reference. Label it with the LogicPlatform namespace and name so the controller can map binding events back to the LogicPlatform.

The implementation must handle Kubernetes' resource-name limits when adding the suffix. Reuse or add a deterministic DNS-safe name helper rather than emitting invalid names for a valid 63-character LogicPlatform name. Keep the Service selector labels stable across changes to user-supplied pod labels.

### 3. Deployment, ports and probes

Create one Deployment in the LogicPlatform namespace using `workflowGatewayName(plat)` and the existing `ToDeploymentSpec` / `ApplicationSpec` path.

- Container name: `workflow-gateway`.
- Container ports: `http` on TCP 8080 and `management` on TCP 8081.
- Set `POD_NAMESPACE` from `metadata.namespace` using a downward-API field reference. Do not rely on the binary's `default` fallback in an in-cluster deployment.
- Keep the gateway's standard restricted container security context unless the user overrides it through the supported ApplicationSpec fields.
- Liveness probe: HTTP `GET /health` on the management port.
- Readiness probe: HTTP `GET /ready` on the management port. A pod must not receive user traffic until the cache and initial catalog are ready.
- Add a startup probe on `/health` if needed to prevent the kubelet from restarting a process while it performs its bounded cache startup. It must not replace the readiness probe.
- Set termination grace to cover the gateway's configured graceful shutdown (default 30 seconds); do not shorten the gateway shutdown timeout below the Kubernetes grace period.

Do not expose port 8081 in the Service. The management server includes `/debug/routes`, so it must remain reachable only through the pod network/exec mechanisms and probes, not through the public service.

The `workflow-gateway` container image entrypoint already supplies default bind addresses. Do not add command-line args unless the API needs a separately designed port override; the Service, probes and container ports must match the binary's existing defaults.

### 4. HPA compatibility and replica safety

Support the same user-managed HPA model documented for Data Index: the HPA targets the generated gateway Deployment directly. When `spec.workflowGateway.application.replicas` is unset, omit `spec.replicas` from the controller's apply configuration so the API defaults the initial Deployment to one replica and the HPA can own subsequent scale changes. Do not default the CR field itself to one, because an explicit value would compete with the HPA. If `application.replicas` is set, honor it as the user's fixed desired replica count; document that values above one are unsupported while the gateway's async instance map remains in memory.

The HPA documentation/example must set `maxReplicas: 1` until the gateway can resolve async instances through shared runtime state. Once that separate capability is implemented, operators can choose a higher maximum. Status replica counts should reflect the live Deployment, including its current HPA-selected count.

### 5. Service and external entry point

Create a ClusterIP Service in the LogicPlatform namespace:

- Service port `http`: 80/TCP, targeting the gateway's `http` container port 8080.
- Selector: the operator-managed gateway selector label.
- Only this port is exposed.

When ingress is absent or `ingress.enabled` is false, delete any existing gateway Ingress/Route and leave the ClusterIP Service available. When enabled, create exactly one front-door resource:

- Kubernetes: an Ingress targeting the gateway Service on port 80.
- OpenShift: a Route targeting the gateway Service on port 80.

Use the existing LogicPlatform Data Index ingress conventions for TLS, annotations, owner references and host handling. Require `host` when `ingress.enabled` is true, consistent with current `DataIndexIngressSpec` validation. Reuse URL resolution helpers where their behavior matches the gateway's needs; report the assigned URL only after the Ingress/Route has a host. Keep URL formatting consistent with `status.dataIndex.service.url` and do not put the URL on LogicFlowRuntime.

No external resource is created when `workflowGateway.ingress.enabled` is false. An external gateway URL is not inferred from the Service DNS name.

### 6. ServiceAccount and RBAC

Create a dedicated ServiceAccount named `workflowGatewayName(plat)` in the LogicPlatform namespace, owned by the LogicPlatform. Set this ServiceAccount on the gateway pod. Do not reuse the operator controller ServiceAccount.

Continue to package a static gateway ClusterRole with the operator installation. The source manifest name `workflow-gateway` is transformed by the existing `logic-operator-` Kustomize prefix to `logic-operator-workflow-gateway`; preserve this rendered name because it matches `ClusterRoleWorkflowGateway`. Verify the installed name in generated manifests and in the dynamic ClusterRoleBinding's `roleRef`.

The gateway ClusterRole is read-only and limited to the resources used by the gateway:

```yaml
- apiGroups: ["logic.kubesmarts.org"]
  resources: ["logicflowservices", "logicflowdefinitions", "logicplatforms"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch"]
```

Lease access currently goes through `mgr.GetClient()`, the manager's cached client, so the cache needs `get`, `list` and `watch` on Leases. Keep the access read-only. Do not grant pod list/watch: the current follow-up routing uses the Lease holder identity as its direct pod destination and has no Pod informer. If a later implementation changes to fetch Pod IPs through a live API reader, grant only the additional `get` on Pods that code requires.

Create one deterministic ClusterRoleBinding per enabled LogicPlatform, binding the dedicated ServiceAccount to the static ClusterRole. The operator itself needs permission to get/create/update/patch/delete these bindings; update the controller RBAC markers and generated install RBAC accordingly. The binding must be repaired if it is deleted or its subject/roleRef drifts.

ClusterRoleBindings cannot be garbage-collected through an owner reference to a namespaced LogicPlatform. Add a LogicPlatform finalizer while the gateway is enabled (and keep it until any binding is removed). On LogicPlatform deletion, delete the binding before removing the finalizer. When `spec.workflowGateway` is removed, remove the binding and gateway resources, then remove the finalizer after cleanup succeeds. Never delete the static ClusterRole as part of one LogicPlatform's cleanup.

The controller must watch/map ClusterRoleBinding changes back to the owning LogicPlatform, using the namespace/name labels and deterministic binding name. Existing `Owns` watches remain appropriate for namespaced Deployment, Service, ServiceAccount and Ingress resources. Add Route ownership/watch only on OpenShift as the controller already does for Data Index.

### 7. Status

Add an optional `LogicPlatformStatus.WorkflowGateway *WorkflowGatewayStatus` field. Keep its refs nested rather than reusing the current top-level `IngressRef`/`RouteRef`, which belong to Data Index.

Add LogicPlatform print columns for gateway readiness and, if useful, its public URL so `kubectl get logicplatform` shows the managed component without requiring a full YAML dump.

`WorkflowGatewayStatus` should report:

- `ready`: true when the gateway Deployment has at least one ready replica and the expected Service exists; false while the enabled component is progressing or missing. This field reports workload/Service readiness, not external Ingress/Route readiness.
- `deploymentRef`, `serviceRef`, and optional `ingressRef` or `routeRef`.
- desired/current/ready/updated replica counts, matching the existing `ReplicaStatus` shape.
- `url`, populated only when an enabled Ingress/Route has a resolvable host. Leave empty for an internal-only gateway or while its host is pending.

Add a `WorkflowGatewayReady` condition with useful reasons for Deployment missing/progressing, Service missing and ready. Remove the condition and clear the status field when `spec.workflowGateway` is absent so a disabled optional component does not make the LogicPlatform appear unhealthy. Publish `url` only after a configured Ingress/Route has a resolvable host; object creation alone is not evidence of an external URL.

Update status using the existing retry-on-conflict pattern. The overall LogicPlatform `Ready` condition and `phase` aggregate every enabled component: Data Index, Vector, and Workflow Gateway. The platform is Ready only when each enabled component's nested `status.ready` is true; disabled components do not gate readiness. Keep each component's own condition and nested readiness field as the source of its detailed status.

### 8. Reconciliation and cleanup order

Add `reconcileWorkflowGateway` to `LogicPlatformReconciler.Reconcile` and `updateWorkflowGatewayStatus` to the status path.

For an enabled gateway, reconcile in this order:

1. Persist the gateway cleanup finalizer if not already present.
2. Reconcile the dedicated ServiceAccount.
3. Reconcile the ClusterRoleBinding.
4. Apply the Deployment and ClusterIP Service.
5. Apply or delete the public Ingress/Route based on `ingress.enabled`.
6. Read Deployment/Service/Ingress/Route state and update nested status and conditions.

For a disabled gateway, delete the gateway Ingress/Route, Service and Deployment; delete the ClusterRoleBinding; delete the ServiceAccount; clear status; then remove the finalizer. Treat NotFound as successful cleanup. Return other API errors so a later reconcile retries. The static ClusterRole is installation-wide and remains installed.

Before applying a deterministic namespaced resource, detect an existing same-name resource that is not labeled/owned by this LogicPlatform. Return a clear reconciliation error instead of silently adopting an unrelated object.

### 9. Scope and multiple LogicPlatforms

Each enabled LogicPlatform gets its own gateway Deployment and public/internal endpoint in that LogicPlatform's namespace. The gateway watches LogicFlowDefinitions and optional LogicFlowServices cluster-wide, while the Data Index endpoint is selected from its own `POD_NAMESPACE`. Preserve that behavior in this issue and document it; do not silently scope gateway discovery to one namespace.

Consequently, multiple enabled LogicPlatforms create multiple gateway front doors, each with its own Data Index endpoint and a cluster-wide workflow catalog. This issue does not define cross-platform Data Index data ownership or enforce a cluster-wide singleton. If that topology is not acceptable, resolve it in a separate gateway discovery/platform-boundary design before enabling the component broadly.

## Acceptance criteria for implementation

- [ ] Setting `spec.workflowGateway` creates an owned Deployment, ServiceAccount, ClusterRoleBinding and ClusterIP Service in the LogicPlatform namespace.
- [ ] The default gateway runs one replica; image default matches the gateway image produced by the repository release flow; explicit ApplicationSpec overrides take effect.
- [ ] A user-managed HPA can target the generated gateway Deployment when `spec.workflowGateway.application.replicas` is unset; document `maxReplicas: 1` until async instance state is shared outside gateway memory.
- [ ] The Deployment sets `POD_NAMESPACE`, exposes container ports 8080 and 8081, probes `/health` and `/ready` on 8081, and is not Ready before the gateway cache/catalog is ready.
- [ ] The Service exposes port 80/TCP targeting gateway pod port 8080 and selects the gateway pods; management port 8081 is not exposed.
- [ ] Enabling ingress creates the correct Kubernetes Ingress or OpenShift Route pointing to the gateway Service on port 80; the URL appears in `LogicPlatform.status.workflowGateway.url` after host assignment.
- [ ] Omitting ingress or setting it disabled leaves the Service internal and removes any prior Ingress/Route and URL.
- [ ] Removing `spec.workflowGateway` removes gateway resources and its ClusterRoleBinding. Deleting the LogicPlatform also removes the binding before finalizer completion.
- [ ] The gateway ClusterRole grants only the read verbs/resources required by the shipped gateway behavior. The operator has the permissions needed to reconcile the namespaced resources and ClusterRoleBinding.
- [ ] No LogicFlowRuntime URL field or sticky-session annotation is added as part of this issue.
- [ ] CRD schema, sample manifests, generated RBAC and docs reflect the new API.
- [ ] Controller/API tests cover defaulting and validation, resource shapes, HPA-compatible replica ownership, external URL status, disabled cleanup, deletion cleanup and name collision behavior.
- [ ] Add a minimal KIND e2e following the existing `test/e2e` patterns (for example `durable_test.go`): deploy the managed gateway with a simple runtime and `sleepy` LogicFlowDefinition (wait 30s), without a LogicFlowService, in a dedicated test namespace. Verify runtime-Service fallback routing by: (1) POST async execution through gateway at /v1/runtime/{namespace}/{definition}/{version}, (2) POST suspend/resume through instance endpoints via the same gateway URL, (3) verify completion by checking that the instance is no longer in active-instances list. This verifies the suspend/resume lifecycle through the gateway and exercises both execution and follow-up routing paths.

## Implementation handoff

1. **API and schema:** Add `WorkflowGatewaySpec`, `WorkflowGatewayStatus`, spec/status fields, defaults, validation and generated CRD/deepcopy output. Add a LogicPlatform sample showing the enabled gateway and ingress configuration.
2. **Resource builders:** Add names, labels/selectors, Deployment and Service builders, dedicated probes/ports/env, plus Kubernetes Ingress and OpenShift Route builders using the existing helpers where appropriate.
3. **RBAC lifecycle:** Align the static ClusterRole name and rules; add ServiceAccount and ClusterRoleBinding reconciliation; add finalizer handling and ClusterRoleBinding-to-LogicPlatform event mapping; update operator RBAC markers/manifests.
4. **Controller/status:** Wire reconciliation, ingress cleanup, nested status/conditions and status URL publication into the LogicPlatform controller.
5. **Tests and docs:** Add unit tests for builders/defaults/validation and controller tests for apply, routing exposure, readiness and cleanup. Update the gateway README and LogicPlatform documentation to describe the management endpoint, external URL field and opt-in semantics.
6. **Generated artifacts and verification:** Regenerate API code/manifests using repository targets, run formatting and the relevant operator plus gateway test suites, and verify install RBAC contains both the gateway ClusterRole and the operator's ClusterRoleBinding permissions.

## Routing behavior to preserve

When documenting or testing the managed gateway, retain the two-path behavior: execution prefers a matching LogicFlowService URL and falls back to the runtime Kubernetes Service derived from `runtimeRef`; follow-up operations use the instance's current lease holder directly. A LogicFlowService, runtime Ingress affinity, and a `LogicFlowRuntime.status` URL are not prerequisites for the gateway-only path.
