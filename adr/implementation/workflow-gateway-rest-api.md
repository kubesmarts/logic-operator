# Workflow Gateway REST API Contract

**Status:** Active — supersedes the REST endpoint section of ADR-0002.
**Date:** 2026-10-07
**Related:** [ADR-0002](https://github.com/kubesmarts/logic-apps/blob/main/adrs/0002-workflow-gateway-architecture.md), Issue [#35](https://github.com/kubesmarts/logic-operator/issues/35)

## Overview

The workflow gateway exposes two ports with distinct responsibilities:

- **`:8080` (traffic port)** — public-facing, routed through Ingress. All paths begin with `/v1/`.
- **`:8081` (management port)** — internal only, never exposed through an Ingress. Health probes and debug endpoints live here.

The URL prefix on the traffic port declares the backend:

| Prefix | Backend |
|--------|---------|
| `/v1/definitions` | Kubernetes CRD store — reads `LogicFlowDefinition` via K8s API |
| `/v1/instances` | Data-index — GraphQL proxy for instance queries |
| `/v1/runtime` | Runtime ingress — execution and instance operations |

## Traffic Port Routes (`/v1/`)

### Definitions (→ CRD store)

```
GET /v1/definitions
GET /v1/definitions/{namespace}/{name}/{version}
```

### Instances (→ data-index)

```
GET /v1/instances
GET /v1/instances/{id}
```

Query parameters for `GET /v1/instances`:
- `workflow` — filter by workflow (`namespace/name` or `namespace/name/version`)
- `status` — filter by instance status (`RUNNING`, `WAITING`, `SUSPENDED`, `PENDING`, `COMPLETED`, `FAULTED`, `CANCELLED`)

### Runtime operations (→ runtime ingress)

```
POST /v1/runtime/{namespace}/{name}
POST /v1/runtime/{namespace}/{name}/{version}

POST /v1/runtime/{namespace}/{name}/instance/{id}/suspend
POST /v1/runtime/{namespace}/{name}/instance/{id}/resume
POST /v1/runtime/{namespace}/{name}/instance/{id}/cancel

POST /v1/runtime/{namespace}/{name}/{version}/instance/{id}/suspend
POST /v1/runtime/{namespace}/{name}/{version}/instance/{id}/resume
POST /v1/runtime/{namespace}/{name}/{version}/instance/{id}/cancel
```

**Version is optional.** When omitted, the gateway returns the URL for any version of that workflow from the routing table. All versions of a workflow are deployed on the same runtime and share a single `LogicFlowService`, so all versions map to the same upstream URL. Version-based traffic splitting is the ingress/runtime's responsibility, not the gateway's.

**`instance` is a literal path delimiter** (singular, not plural). It unambiguously marks the boundary between the workflow identity and the instance ID, allowing chi's router to distinguish `/{version}` from `/instance/{id}/…` without content inspection.

**Headers:**
- Request: `X-Flow-Route: <cookie-value>` (optional) — pod-level routing hint
- Response: `X-Flow-Route: <cookie-value>` — updated routing hint from runtime

### API Spec

```
GET /v1/openapi.json
```

Returns the OpenAPI 3.0 spec for all traffic-port routes as JSON.

## Management Port Routes (`:8081`)

```
GET /health    — liveness probe; always 200 OK
GET /ready     — readiness probe; 200 OK after cache sync, 503 before
GET /debug/routes — JSON snapshot of the current routing table
```

## Key Design Decisions

1. **Port separation** — Management endpoints are firewalled from traffic endpoints. Kubernetes probes target `:8081`; Ingress routes to `:8080`.
2. **`instance` delimiter** — Using the plural `instances` would imply a collection resource. Runtime operations never browse a collection; the singular literal avoids that implication and also resolves the version-optional path ambiguity.
3. **Queries vs operations split** — `GET /v1/instances` routes to data-index; `POST /v1/runtime` routes to the runtime ingress. This mirrors the ADR-0002 principle: queries go to data-index, writes go to the runtime.
4. **Version-optional via `LookupLatest`** — When the version segment is absent from a `/v1/runtime/` path, the gateway calls `RouteStore.LookupLatest(namespace, name)`, which returns the URL for any known version of that workflow. All versions share a single `LogicFlowService` and therefore the same upstream URL; the ingress is responsible for traffic splitting.
