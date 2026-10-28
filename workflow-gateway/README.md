# Workflow Gateway

The Workflow Gateway is an HTTP entry point for Logic Operator workflows. It discovers
`LogicFlowDefinition` and `LogicFlowService` resources, routes workflow execution to Quarkus Flow
Runner, and forwards instance lifecycle operations. It can also read workflow definitions from the
Kubernetes API and query workflow instances through Data Index.

For the user guide, API routes, LogicPlatform integration, and known limitations, see the
[Workflow Gateway documentation](../docs/antora/modules/ROOT/pages/components/workflow-gateway.adoc).

## Routing behavior

For execution, a matching `LogicFlowService.status.url` takes precedence and preserves that
service's ingress or traffic policy. Otherwise, the gateway uses the workflow's
`LogicFlowDefinition.spec.runtimeRef.name` and calls the runtime's Kubernetes Service on port 80
(runner container port 8080). A `LogicFlowService` is optional.

Async execution stores an in-memory instance-to-lease identity mapping. Suspend, resume, and cancel
resolve the current lease holder and call the runner pod on port 8080; if that lookup is not
available, the gateway falls back to the runtime Service. Keep the gateway at one replica until
this state is shared across replicas. An HPA may be used with `maxReplicas: 1`.

## Build and run

Build the binary from the repository root:

```bash
make build-gateway
```

Build the container image using the independently configured `GATEWAY_VERSION` in `versions.mk`:

```bash
make docker-build-gateway
```

The default image tag is `kubesmarts.org/workflow-gateway:v$(GATEWAY_VERSION)`. When running in
Kubernetes, `LogicPlatform` defaults the gateway image to the version embedded in the operator
build. Set `spec.workflowGateway.application.image` to override it.

To run the binary locally, provide a usable Kubernetes configuration and access to the Logic
Operator CRDs and resources. The gateway uses `POD_NAMESPACE` to locate the LogicPlatform for
Data Index discovery; it defaults to `default` when unset.

The public API listener defaults to `:8080`. The management listener defaults to `:8081`.
Containerized development runs can publish both ports:

```bash
docker run --rm -p 8080:8080 -p 8081:8081 kubesmarts.org/workflow-gateway:v<version>
```

## Listeners

Public API routes are under `/v1`; retrieve the OpenAPI specification at `/v1/openapi` or
`/v1/openapi.json`.

The management listener provides:

| Path | Behavior |
|------|----------|
| `/health` | Liveness check; returns 200 while the process responds. |
| `/ready` | Returns 200 after the cache and initial routing table are synchronized; otherwise 503. |
| `/debug/routes` | Shows the current workflow execution endpoint table. |

The LogicPlatform-managed Service exposes only port 80 targeting API port 8080. Port 8081 is used
by container probes and is not exposed by that Service.

## Data Index behavior

`GET /v1/instances` and `GET /v1/instances/{id}` use the GraphQL endpoint published by the
LogicPlatform in the same namespace as the gateway. They return 503 if Data Index is disabled or
not yet discovered. Workflow execution does not depend on Data Index.

## Current limitations

* Async instance routing state is held in process memory. Use one gateway replica (`maxReplicas: 1`)
  until this mapping uses shared runtime state.
* A gateway restart loses its instance mappings; follow-up requests then fall back to the runtime
  Service, which might not select the pod holding an in-memory instance.
* The gateway forwards requests to the runner and does not define a separate authentication policy.
  Configure runtime security and secure the external Ingress or Route.
