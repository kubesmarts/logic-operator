# Workflow Gateway CRD Discovery & Watching

**Status:** Design Document
**Version:** v1.0
**Last Updated:** 2026-10-05
**Related:** [ADR-0002: Workflow Gateway Architecture](https://github.com/kubesmarts/logic-apps/blob/main/adrs/0002-workflow-gateway-architecture.md), Issue [#34](https://github.com/kubesmarts/logic-operator/issues/34)

## Overview

The workflow gateway resolves an incoming request's workflow identity to the external ingress URL that fronts the executing runtime, then forwards the request **through that ingress**. To do this without a per-request API call, the gateway watches the relevant CRDs and maintains an in-memory routing table. This document records the design decisions behind that discovery mechanism and corrects a model mismatch between ADR-0002 and the current CRDs.

## Table of Contents

1. [Context](#context)
2. [Key Correction: Where the External URL Lives](#key-correction-where-the-external-url-lives)
3. [Resource Model and the Join](#resource-model-and-the-join)
4. [Decision 1: In-Memory Cache via Informers](#decision-1-in-memory-cache-via-informers)
5. [Decision 2: No Cross-Pod Clustering](#decision-2-no-cross-pod-clustering)
6. [Decision 3: Watch Two CRDs, Forward Through Ingress](#decision-3-watch-two-crds-forward-through-ingress)
7. [Settled Decisions](#settled-decisions)
8. [RBAC](#rbac)
9. [Testing Strategy](#testing-strategy)

---

## Context

The gateway is a separate application from the operator (separate `go.mod`, separate Deployment). The operator *owns* and writes the CRDs; the gateway *reads* them. This is the standard Kubernetes operator-as-config-provider pattern (cert-manager, ingress-nginx, Istio, prometheus-operator all work this way): the API server is the single source of truth, and the data-plane component derives read-only state from it.

Issue #34 asks for a mapping `(namespace, name, version) → ingress URL`, built at startup and kept in sync with CRD events, rejecting cluster-internal Service DNS names in favor of external ingress endpoints.

## Key Correction: Where the External URL Lives

ADR-0002 states the operator must populate **LogicFlowRuntime** with the external ingress URL. **The current CRDs do not work this way.**

- `LogicFlowRuntimeStatus` (`api/v1/logicflowruntime_types.go:40-86`) has **no external URL**. It exposes only `ServiceRef` — the internal ClusterIP Service — which is precisely the `*.svc.cluster.local` form ADR-0002 says to reject.
- The external URL lives on `LogicFlowService.Status.URL` (`api/v1/logicflowservice_types.go:131-133`). LogicFlowService is the resource that creates the Ingress/Route/HTTPRoute and owns the external host.

**Consequence:** the gateway must watch **LogicFlowService** to obtain routing targets. Routing to the Runtime directly would mean routing to a ClusterIP, bypassing TLS, traffic splitting, sticky sessions, and observability — the exact failure ADR-0002 warns against. ADR-0002 should be updated to reflect that the URL is a Service-level property.

**Corollary — LogicFlowRuntime is not on the routing path.** Because the external URL comes from the Service and the request is forwarded *through* the Service's ingress, the gateway never addresses the Runtime directly. The Runtime is the execution workload; its readiness is already reflected by the ingress (503 when no backends) and by the data-index 404 fallback. The gateway therefore watches **only LogicFlowService and LogicFlowDefinition**.

## Resource Model and the Join

```
LogicFlowService          status.url = https://hello.apps.cluster.example.com   (external URL)
  ├─ spec.defaultDefinition.name ──┐   (100% traffic to one definition)
  └─ spec.traffic[].definitionRef.name ┘ (weighted split across definitions)
                             │  forward reference, by NAME, in the Service's namespace
                             ▼
LogicFlowDefinition       metadata.labels.{workflow-namespace, workflow-name, workflow-version}  (identity tuple)

  (LogicFlowRuntime — execution workload; NOT on the routing path, not watched)
```

The routing table is a **join across two resources, driven by the Service**:

| Resource | Contributes |
|----------|-------------|
| LogicFlowService | external URL (`status.url`), traffic weights, and the explicit list of definition names it fronts (`spec.defaultDefinition` / `spec.traffic[].definitionRef`) |
| LogicFlowDefinition | identity tuple `(workflowNamespace, workflowName, workflowVersion)` read from labels |

**Join direction: Service → Definition.** The Service is the anchor. It explicitly references the Definitions it exposes by name, within its own namespace (`LocalObjectReference`). For each referenced Definition we read the identity tuple from its labels (`LabelWorkflowName`, `LabelWorkflowVersion`, `LabelWorkflowNamespace` in `logicflowdefinition_types.go:31-36`). This mirrors the operator's own `resolveDefinitions` (`logicflowservice_controller.go:274-303`).

Resulting map: `(workflowNamespace, workflowName, workflowVersion) → LogicFlowService.Status.URL`.

**Definition names are arbitrary.** A customer may name a definition `workflow1-v2.0.0` or anything else — the name is *not* the routing key and carries no reliable structure. The key comes only from the Definition's labels, which is why the Service's explicit `definitionRef` is the only sound way to find the right Definition. (An earlier draft matched Service to Definition by a fabricated `namespace/name` convention; that was wrong and has been removed.)

LogicFlowRuntime is intentionally excluded: `Definition.spec.runtimeRef` identifies the executor, but the gateway never needs it because it forwards through the Service's ingress rather than addressing the Runtime.

## Decision 1: In-Memory Cache via Informers

**Decision:** Build the routing table in memory from an informer (watch) cache. Do not query the API server per request.

**Rationale:**
- A per-request `Get`/`List` adds an uncontrolled network hop + deserialization to every user request's tail latency.
- It loads a shared cluster resource; API Priority & Fairness will throttle the gateway under load, stalling user requests.
- It couples the data path to API-server availability. A routing table must keep serving from memory even if the API server is briefly unreachable.

The informer does one initial `List` (full sync) then streams deltas via `Watch`; lookups become O(1) map reads. The only cost is a small eventual-consistency window (typically single-digit milliseconds behind the API server).

**Keep the cache lean — strip `Definition.Spec` on ingest.** `LogicFlowDefinition.Spec.Flow` is an arbitrarily large Open Workflow Spec document the routing join never reads; with hundreds of workflows, caching every full Definition would waste significant per-pod memory. controller-runtime's cache applies a per-object transform before an object is committed to the informer store (`cache.Options.ByObject[&LogicFlowDefinition{}].Transform`). The gateway uses this to zero `Spec` on every Definition as it enters the cache, so the flow document is never held in memory on any pod. LogicFlowService is cached in full (it is lightweight and the join needs both its spec and status). Both resources are listed from the cache during recompute — no per-request or per-recompute API calls. The routing table needs only Service spec/status plus Definition labels, all of which survive the transform.

**Routing table ≠ Discovery API.** Serving full workflow definitions to clients (list/get) is a *separate* concern — ADR-0002's "CRD Discovery → Kubernetes API" row — handled by a direct `List`/`Get` against the API server, not from this cache. That is tracked as issue #36. This document and issue #34 cover only the routing table.

## Decision 2: No Cross-Pod Clustering

**Decision:** Scale gateway pods independently. No leader election, shared store, or pod-to-pod sync.

**Rationale:** The API server is the synchronization point. Each pod runs its own informer, watches the same CRDs, and converges to an identical in-memory map. Clustering would only be required if the gateway held **write state** (session ownership, rate-limit counters, instance→pod assignments). It does not — the routing table is read-only derived data, and per-request stickiness is delegated to the ingress layer (`X-Flow-Route` → cookie). Read-only derived state + watch = no sync required. This is what makes the "fully stateless" claim in ADR-0002 hold.

**Nuance:** during rollouts or immediately after a workflow is created, pods may briefly sit at slightly different cache versions. For read-only routing this is benign — a just-created workflow may 404 on a lagging pod for a few milliseconds until its watch catches up. ADR-0002's "query data-index on 404 to disambiguate" already covers this edge.

## Decision 3: Watch Two CRDs, Forward Through Ingress

**Decision:** Watch LogicFlowService and LogicFlowDefinition. Resolve to the Service's external ingress URL and forward **through** the ingress — never to a ClusterIP or pod IP. LogicFlowRuntime is not watched (see corollary above).

**Rationale:** Forwarding through the ingress preserves TLS termination, canary/weighted traffic splitting, sticky sessions, and observability. This is a deliberate extra hop (Client → Gateway → runtime Ingress → pod) accepted in exchange for keeping traffic management intact.

**Implementation note:** the repo already depends on `sigs.k8s.io/controller-runtime v0.24.1`, and the CRD Go types live in the shared `api/v1` module. The gateway imports `api/v1` and uses controller-runtime's cache (informers without reconcilers) to get typed, auto-resyncing, auto-reconnecting watches on both CRDs — the same machinery the operator uses, with no leader election for read-only caching. Event handlers on both informers trigger a debounced recompute; the recompute lists Services and resolves referenced Definitions via `Get`.

## Settled Decisions

1. **Version weighting is an ingress concern, not a gateway concern.** `LogicFlowService.Spec.Traffic[]` holds weighted splits across definition versions, realized by the Service's HTTPRoute/Ingress. When a request does **not** pin a version, the gateway routes to the Service URL and lets the ingress perform the weighted split — the gateway does not re-implement weighting. Only a version-pinned request resolves to a version-specific route.
2. **The gateway becomes ready with an empty map.** It populates as CRDs appear rather than blocking on at-least-one-CRD (which may never arrive if the operator is down). Readiness reflects "watches established," not "routing table non-empty."
3. **URL validation rejects cluster-internal Service DNS, scheme-agnostic.** Reject `*.svc.cluster.local` and the bare `*.svc` / in-cluster forms on `LogicFlowService.Status.URL`. The `https` scheme is **not** forced — plain HTTP is required for kind/dev environments. HTTPS enforcement, if wanted later, is added as an opt-in configuration flag rather than hardcoded. A rejected URL drops only that entry (it does not fail the whole sync).

## RBAC

Minimal read-only access in the CRD API group:

```yaml
- apiGroups: ["logic.kubesmarts.org"]
  resources: ["logicflowservices", "logicflowdefinitions"]
  verbs: ["get", "list", "watch"]
```

## Testing Strategy

- **Unit:** map mutations (add/update/delete) across the Service↔Definition join; URL validation (reject cluster-internal Service DNS; accept both HTTP and HTTPS external URLs); version-pinned vs unpinned resolution.
- **Integration:** fake client + informer to simulate operator-driven create/modify/delete event ordering, including Definition arriving before or after its Service.
- **E2E:** real operator + real gateway pods to catch timing/consistency issues a mocked watch hides.
- **Resilience:** watch disconnect/reconnect; CRD deleted mid-request (expect fallback-to-data-index path, not a hard failure).
