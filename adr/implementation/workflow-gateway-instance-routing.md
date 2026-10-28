# Workflow Gateway — Lease-Based Instance Follow-Up Routing (Direct-to-Pod)

**Status:** Active — supersedes Decision 2 (No Cross-Pod Clustering, as it pertains to the forwarding path) and Decision 3 (Forward Through Ingress) of `workflow-gateway-crd-discovery.md`, and the runtime-forwarding model of [ADR-0002: Workflow Gateway Architecture](workflow-gateway-architecture.md). Those documents have been updated.

**Date:** 2026-10-16

**Related:** Issue [#38](https://github.com/kubesmarts/logic-operator/issues/38), [adr/implementation/workflow-gateway-crd-discovery.md](workflow-gateway-crd-discovery.md), [adr/implementation/workflow-gateway-rest-api.md](workflow-gateway-rest-api.md)

> **Current execution-path note (2026-10-27):** The gateway-only deployment introduced for issue #42 does not require a `LogicFlowService`. Execution prefers `LogicFlowService.status.url` when a matching resource exists and otherwise calls the runtime Kubernetes Service identified by `LogicFlowDefinition.spec.runtimeRef`. Instance follow-up operations still resolve the lease holder and call that pod directly. This supersedes Decision 6 below for the start/synchronous execution path; the direct-to-pod follow-up rationale remains active.

## Overview

Workflow runtimes (quarkus-flow) hold workflow-instance state in memory, partitioned by lease — the lease name equals the `workflowApplicationId` (the shard). Only the pod holding a given lease services instances in that shard. Instance-addressed operations (suspend, cancel, resume) must reach the specific pod that currently holds the instance's lease. This document records the decision to route those follow-up operations directly to the owning pod, bypassing the ingress.

## Table of Contents

1. [Context](#context)
2. [Decisions](#decisions)
3. [Consequences & Tradeoffs](#consequences--tradeoffs)
4. [Prerequisites](#prerequisites)
5. [Open & Deferred Items](#open--deferred-items)

---

## Context

**Lease and sharding model:**
- Workflow runtimes partition instance state in memory by lease: `leaseId == workflowApplicationId == shard`
- Only the pod holding a given lease services instances in that shard
- Durable mode is backed by PostgreSQL; the runtime DB holds `instanceId -> workflowApplicationId` (i.e., instanceId → leaseId)
- Leases exist only when persistence is configured

**Runtime exposure:**
- The runtime exposes a single ClusterIP Service per LogicFlowRuntime that round-robins across all pods; it is not per-pod addressable
- The external URL lives on `LogicFlowService.Status.URL` (the object that creates Ingress/Route/HTTPRoute)

**Prior design problem:**
- Gateway traffic forwarded **through** the ingress, relying on nginx/HAProxy cookie affinity (sticky sessions) to pin a client to a pod
- Because the gateway is an in-cluster component (singleton in an infra namespace), it had to call the workflows' **public ingress endpoints** — a hairpin (traffic exiting toward the edge and returning)
- Cookie affinity is also best-effort CLIENT-session stickiness, not authoritative INSTANCE ownership
- Instance-addressed operations (suspend, cancel, resume) depend on reaching the specific pod holding the lease; sticky-session affinity is insufficient

## Decisions

1. **Gateway as an entry point:** The workflow-gateway can be the entry point for workflow operations. Execution uses a configured LogicFlowService URL when one matches the workflow, with the runtime Kubernetes Service as fallback; instance-addressed follow-up operations route **directly to the owning pod** over the cluster network, bypassing the ingress.

2. **Routing model:** `instanceId → leaseId → pod`, where `leaseId == workflowApplicationId == shard`.
   - `instanceId → leaseId`: returned in the async `/exec` response as the `workflowApplicationId` field; the authoritative copy lives in the runtime's PostgreSQL (`instanceId → workflowApplicationId`)
   - `leaseId → pod`: resolved **live** from the Kubernetes Lease object's `HolderIdentity` (the holding pod's name) and then the pod's IP. It is never cached, so pod failover requires no routing-table update — the next lookup is already current.

3. **Pod resolution mechanism:** Pod resolution is done with two targeted Kubernetes GETs per follow-up call: get the Lease by its deterministic name, get the Pod by HolderIdentity name to read `status.podIP`. No Pod/Lease informer cache is required initially because follow-up operations are low-frequency relative to start. If volume later warrants, upgrade to label-scoped informers: pods carry `app.kubernetes.io/part-of=logic-platform`; member leases carry `app.kubernetes.io/managed-by=quarkus-flow`.

4. **Instance-to-leaseId cache:** The gateway caches `instanceId → leaseId` in a per-replica in-memory LRU bounded by SIZE (configurable at gateway startup, e.g. ~20MB), not by a time-based TTL (a TTL may exist only as a secondary backstop). The cache is a pure latency optimization: PostgreSQL is the source of truth, so cache loss and gateway scale-out are harmless. An entry is evicted when a CANCEL returns OK (the instance is terminal).

5. **Cross-replica state:** Go processes do not share memory across replicas. In persistence mode, PostgreSQL IS the shared store — any replica resolves `instanceId → leaseId` from the DB on a cache miss, and clients stay dumb (they send only workflow identity + instanceId). In NON-persistence mode there is no shared store, so the client must pass the `workflowApplicationId` back on follow-up calls, or the gateway must run as a single replica. **No distributed-cache dependency** (Redis/Infinispan) is introduced.

6. **Historical start-path decision:** `/exec` also goes through the gateway direct-to-pod. This decision was superseded by the 2026-10-27 execution-path note above: configured LogicFlowService URLs are preferred, with the runtime Kubernetes Service as fallback when no LogicFlowService exists.

7. **Failover handling:** Because `leaseId → pod` is resolved live, during a failover window (up to the lease duration) `HolderIdentity` may still name a dead/terminating pod or be briefly empty. Direct pod calls MUST be wrapped in retry-with-backoff; once a new pod re-acquires the lease, `HolderIdentity` updates and the retry succeeds.

8. **Scale-down orphans:** If a lease slot that still holds live instances is removed during scale-down, the instance becomes orphaned in the DB. The gateway returns HTTP 404 with a message indicating the instance is dangling/unrecoverable.

9. **Sync vs async paths:** A synchronous `/exec` holds the HTTP connection on the handling pod for the instance's whole lifetime, so there are no out-of-band suspend/cancel/resume operations and no mapping is needed. Only ASYNC instances require the `instanceId → leaseId` mapping.

## Consequences & Tradeoffs

| Consequence | Impact |
|-----------|--------|
| **Cookie affinity removed** | Session-stickiness cookie affinity is REMOVED from the operator's LogicFlowService Ingress/Route generation (done on the issue #38 branch). |
| **In-cluster TLS deferred** | In-cluster TLS on the direct pod hop is NOT supported for now (rarely requested; deferred — a service mesh or runner-served TLS can provide it later). |
| **Traffic-splitting deferred** | Weighted traffic-splitting via the gateway is deferred (see Decision 6). |
| **Auth pass-through** | The gateway passes client-supplied tokens THROUGH to the runner, which evaluates them; a dedicated gateway auth design is a future issue. |
| **Observability gap** | Direct pod calls bypass ingress access logs, so the gateway must propagate OpenTelemetry trace context to the pod. A follow-up issue should be filed in logic-operator to implement OTel on the gateway path. |
| **Ingress retained optional** | LogicFlowService + Ingress/Route remain optional for gateway execution. The gateway uses a configured LogicFlowService URL when present and can route directly to the runtime Service when absent. Non-gateway users continue to use LogicFlowService directly. |

## Prerequisites

- **RBAC:** The gateway needs `watch`/`get` on `coordination.k8s.io` Leases and `core` Pods across the relevant namespaces (aligns with the existing workflow-gateway discovery ClusterRole, issue #62).
- **Network:** Pod-to-pod traffic across namespaces must be reachable (a NetworkPolicy allowance is required if policies restrict it).
- **Runtime contract:** The async `/exec` response must include `workflowApplicationId` (the leaseId), and PostgreSQL must hold `instanceId → workflowApplicationId`.

## Open & Deferred Items

- In-cluster TLS on the direct pod hop
- Weighted traffic-splitting via the gateway
- Dedicated gateway authentication/authorization design
- OpenTelemetry implementation on the gateway path (file an issue in logic-operator)
- Scale-down orphan recovery semantics
