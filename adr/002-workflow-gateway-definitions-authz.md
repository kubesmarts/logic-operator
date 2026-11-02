# ADR 002: Workflow Gateway `/v1/definitions` Authentication & Authorization

**Date**: 2026-11-02
**Status**: Proposed
**Scope**: `workflow-gateway` — the `/v1/definitions` endpoints only

---

## Context

The workflow gateway exposes `GET /v1/definitions` (list) and
`GET /v1/definitions/{namespace}/{name}/{version}` (fetch) with **no
authentication or authorization** today.

These handlers serve `LogicFlowDefinition` data that the gateway reads from the
Kubernetes API server using its **own, broadly-privileged ServiceAccount** — an
informer cache spanning every namespace the gateway watches, plus an on-demand
`apiReader.Get` for the full flow document (see
`workflow-gateway/internal/server/server.go` and
`workflow-gateway/internal/definitions/reader.go`).

The result is a **confused-deputy / privilege-escalation** hole: any anonymous
caller borrows the gateway's elevated cluster read access and can enumerate every
workflow definition in every watched namespace. Workflow definitions are not
innocuous — they embed business logic, service endpoints, and secret references.

### Out of scope

`/v1/runtime/*` (execute, sync, suspend, resume, cancel) and `/v1/instances/*`
are **not** addressed here. Those requests are forwarded to backend runners /
data-index services that already implement the quarkus-flow security model; the
gateway simply passes their auth headers through. This ADR is strictly about
putting an authorization gate in front of the `definitions` endpoints so a caller
sees only what its identity is entitled to.

### Clients (planned)

- **Operational console** — an OpenShift console plugin. Calls arrive with the
  user's OpenShift OAuth token (an **opaque `sha256~…` bearer**, not a JWT).
- **Authoring console** — a browser app where the user configures a Kubernetes
  connection stored locally and pushes k8s credentials on each call. We control
  this client, so we pin it to an SA or opaque token.
- **Other APIs / dashboards** — machine-to-machine clients using the quarkus-flow
  `API_KEY` or `OIDC` modes.

---

## Decision

Introduce two authorization paths in front of `/v1/definitions`, selected
deterministically per request, both of which filter results down to the
namespaces the caller is entitled to:

1. **Kubernetes-native path** — authorization delegated to the cluster.
2. **Quarkus-flow path** — the gateway is the policy decision point, mirroring the
   [quarkus-flow runner security model](https://docs.quarkiverse.io/quarkus-flow/dev/runner.html#security-modes)
   verbatim so credentials are interchangeable between runner and gateway.

### 1. Kubernetes-native authorization

For k8s credentials, authorization is the user's **real RBAC**, enforced by the
API server — no parallel source of truth, no drift.

- Authenticate the token via **`TokenReview`**.
- Filter the definition set with **`SelfSubjectAccessReview`** (created with the
  **caller's own** token): for each candidate namespace, check whether the user
  may `list` (list endpoint) / `get` (fetch endpoint) `logicflowdefinitions`.
  Only authorized namespaces are returned.
- This adds **no RBAC on the gateway ServiceAccount** (no impersonation) and
  **no extra RBAC on the user**: `system:authenticated` may create
  `SelfSubjectAccessReview`, and `list/get logicflowdefinitions` is exactly the
  permission the user would already need to `kubectl get logicflowdefinitions`.
  The gateway refuses to leak past the RBAC the API server already defines.

Rejected alternatives:

- **Token forwarding / re-query as the user** — correct, but discards the cache
  and costs an API round-trip per request.
- **Impersonation** — requires the scary `impersonate` privilege on the gateway
  SA and is harder to audit.
- **Coarse "can access the namespace at all" check** — over-discloses: a user
  scoped to only (say) configmaps in a namespace would receive its workflow
  definitions, and the proxy verb is arbitrary.

#### Companion change: aggregate the shipped RBAC roles

The project ships `logicflowdefinition-viewer-role` / `-editor-role` /
`-admin-role` as **standalone** ClusterRoles with no aggregation labels. As-is, a
user with generic namespace `view` does **not** automatically get
`list logicflowdefinitions`, so the SSAR check would be operationally brittle
(every user needs an explicit extra binding).

Fix: add the standard aggregation labels
(`rbac.authorization.k8s.io/aggregate-to-view|edit|admin: "true"`) to those
ClusterRoles. Then anyone with standard `view`/`edit`/`admin` on a namespace
inherits LFD read, and the precise SSAR check "just works" with no per-user
wrangling — least privilege **without** the brittleness.

### 2. Quarkus-flow authorization (mirror the runner verbatim)

Configured under the platform (gateway spec), **single-mode** like the runner —
exactly one of `NONE` / `API_KEY` / `OIDC`. Reuses and extends the existing
`RuntimeSecuritySpec` family in `api/v1`.

**Roles** (unchanged from the runner):

- `flow-admin` — all namespaces; ignores any namespace restriction.
- `flow-invoker` — restricted to the authorized namespaces; a **missing / null /
  empty / blank** namespace scope grants **no** access (→ `403`), never "all".

**OIDC — exact JWT contract (must match the runner so tokens are interchangeable):**

- Roles from a **top-level `roles`** claim containing `flow-admin` / `flow-invoker`.
  *Not* `groups`, `realm_access.roles`, `resource_access`, or `scope`.
- Namespace from a **`namespace`** claim (configurable name); value may be a
  single string, an array, a comma-separated string, or `"*"`.
- Example payload:
  `{"sub":"alice@example.com","roles":["flow-invoker"],"namespace":["examples","team-a"]}`
- Standard Quarkus OIDC config (`auth-server-url`, `client-id`, secret),
  sourced from the platform default or a per-gateway override.

**API_KEY contract:**

- Credential in **`Authorization: Bearer <api-key>`** (same header as JWTs — the
  runner does not use a separate `X-API-Key` header).
- Roles and namespaces assigned **per key** in config; omitted namespaces → `"*"`.

**Errors:** `401` for missing/invalid credentials; `403` when authenticated but
not authorized for the namespace. `/v1/openapi*` stays unauthenticated for
discovery, matching the runner.

**Accepted risk (OIDC drift):** on the OIDC/API_KEY path the gateway trusts the
`namespace` claim / key config as a **parallel** authorization source that can
drift from real cluster RBAC. This is why the authoring console uses the k8s path
(real RBAC). The drift is accepted for machine-to-machine and SSO clients.

### 3. Deterministic request routing (route ≠ trust)

All three credential types share `Authorization: Bearer`. The gateway selects a
single validator per request via a **local, unverified decode** of the token used
**only to route**. The chosen authority then **verifies** before any claim is
trusted — so routing can never be exploited the way trusting-on-sight could (a
forged `roles:[flow-admin]` still has to survive signature verification).

When OIDC is configured under the gateway spec:

1. **Opaque bearer** (not parseable as a JWT) → **kube** (`TokenReview`).
   *(OpenShift console token.)*
2. **JWT with kube markers** (`kubernetes.io` claim / cluster SA issuer) →
   **kube**. *(ServiceAccount token.)*
3. **JWT whose `roles` claim carries `flow-admin`/`flow-invoker`** → **OIDC
   server** (verifies signature, then trusts `roles`/`namespace`).
4. **JWT with none of the above** → **kube** (`TokenReview`).

In `API_KEY` mode there is no JWT ambiguity: match the bearer against configured
keys; no match → kube. In `NONE` mode there is no quarkus-flow authz; only the
kube path applies (dev only).

This is **one local decode → one delegation** — never "try many servers."

#### Why this closes the external-OIDC edge case

A vanilla-k8s user authenticated via an external IdP (`kubectl` + OIDC) sends the
**raw IdP id_token**; kube never stamps it, so it carries **no** kube markers and
**no** `flow-*` roles. It lands in **step 4** and is delegated to kube, which
validates it against the **cluster's own** OIDC issuer and maps it to RBAC. This
works for any real kube user by definition (the API server must already trust that
issuer, or the user could not reach the cluster at all).

| Credential | Minted by | Marker | Route |
|---|---|---|---|
| OpenShift user token | OpenShift OAuth | opaque `sha256~…` | kube (step 1) |
| ServiceAccount token | kube `TokenRequest` | `kubernetes.io` claim, SA issuer | kube (step 2) |
| Quarkus-flow OIDC token | Gateway's IdP | `roles: [flow-*]` | OIDC (step 3) |
| External-OIDC kube user | External IdP | none | kube (step 4) |

**Residual (shared-IdP):** if the cluster's OIDC IdP *is* the gateway's OIDC
server *and* its mapper stamps `flow-*` roles, such a token deterministically
takes step 3 (claims path). Fully deterministic and documented.

---

## Consequences

**Positive**

- Closes the confused-deputy hole on `/v1/definitions`.
- k8s path has zero authz drift and needs no new privileges on the gateway SA.
- Wire-compatible with quarkus-flow runner credentials.
- Deterministic routing: one local decode, one delegation.
- Role-aggregation fix makes least-privilege the easy default.

**Negative / costs**

- SSAR adds up to one API-server check per candidate namespace per request
  (mitigable with short-lived per-identity caching).
- OIDC/API_KEY path carries accepted authz drift vs. real RBAC.
- Requires extending `RuntimeSecuritySpec` (namespace scoping on API keys,
  `namespace` claim mapping, gateway wiring) and the routing layer.

---

## Implementation notes (non-binding; the implementer owns these)

- Extend `api/v1` security types: namespace scoping on `APIKeySpec`, OIDC
  `namespace`/`roles` claim-name config, and a gateway security spec under the
  platform.
- Add auth middleware in `workflow-gateway/internal/server` wrapping the
  `/v1/definitions` routes only; keep `/v1/openapi*` open.
- Namespace filtering applies to both the list snapshot and the single-definition
  fetch (reject with `403` if the caller lacks the definition's namespace).
- Add aggregation labels to `config/rbac/logicflowdefinition_{viewer,editor,admin}_role.yaml`.
- Short-lived caching of SSAR decisions per (identity, namespace) is a reasonable
  optimization but not required for correctness.
