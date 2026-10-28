# Logic Operator

A Kubernetes operator for managing Serverless Logic workflows powered by Quarkus Flow.

## Description

The Logic Operator provides a Kubernetes-native way to deploy, manage, and scale Serverless Logic workflows. Built on top of Quarkus Flow, it offers four core CRDs for defining and orchestrating workflow-based applications:

- **LogicPlatform**: Data Index, optional Vector, and optional Workflow Gateway management
- **LogicFlowService**: External workflow endpoint and optional traffic-split policy
- **LogicFlowDefinition**: Workflow definitions and specifications
- **LogicFlowRuntime**: Runtime instances of deployed workflows

This repository tracks the 2.0 development line. Operator and component versions are declared in `versions.mk`.

The standalone Workflow Gateway accepts execution and lifecycle requests and can route directly
to a workflow's runtime Service. See the [Workflow Gateway guide](docs/antora/modules/ROOT/pages/components/workflow-gateway.adoc) for its API, LogicPlatform integration, and current scaling limitations.

## Getting Started

### Prerequisites

- [Go](https://go.dev/dl/) v1.26+
- [Docker](https://docs.docker.com/get-docker/) v17.03+
- [kubectl](https://kubernetes.io/docs/tasks/tools/) v1.30+
- [KIND](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) v0.20+

### Quickstart (KIND)

Get a fully working operator with sample workflows in three commands:

```sh
make kind-create    # create KIND cluster with ingress-nginx + cert-manager
make kind-deploy    # build, load, and deploy the operator
make kind-demo      # build/load the gateway image and apply Platform + Runtime + Definition
```

Check that everything is running:

```sh
kubectl get logicplatforms,logicflowruntimes,logicflowdefinitions
```

The sample deploys a `hello-world` workflow. Because it does not create a `LogicFlowService`, the gateway routes directly to the runtime's Kubernetes Service:

```sh
kubectl wait --for=condition=available deployment/hello-runtime --timeout=120s
kubectl wait --for=condition=available deployment/logicplatform-sample-workflow-gateway --timeout=120s
curl -X POST http://gateway.lvh.me/v1/runtime/default/hello-world/1.0.0/sync \
  -H "Content-Type: application/json" \
  -d '{"name": "World"}'
```

> `lvh.me` resolves to `127.0.0.1` — no `/etc/hosts` editing needed.

To inspect the runtime's OpenAPI spec or health checks, start this port-forward in a separate terminal:

```sh
kubectl port-forward svc/hello-runtime 8080:80
curl http://localhost:8080/q/openapi   # OpenAPI spec
curl http://localhost:8080/q/health    # health checks
```

### Enable API-KEY Authentication

To secure the runtime with API-KEY authentication, apply the authentication overlay on top of the demo:

```sh
kubectl apply -k config/samples/authentication/
kubectl rollout restart deployment/hello-runtime
kubectl rollout status deployment/hello-runtime --timeout=120s
```

Unauthenticated requests are now rejected:

```sh
curl -s -o /dev/null -w "%{http_code}" -X POST http://gateway.lvh.me/v1/runtime/default/hello-world/1.0.0/sync \
  -H "Content-Type: application/json" -d '{"name": "World"}'
# 401
```

Pass the API key as a Bearer token:

```sh
curl -X POST http://gateway.lvh.me/v1/runtime/default/hello-world/1.0.0/sync \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer logic-operator-dev-token" \
  -d '{"name": "World"}'
```

### Enable Persistence (PostgreSQL + Durable Workflows)

Deploy a 3-replica runtime with PostgreSQL persistence and a sleepy workflow for testing durable execution:

```sh
kubectl apply -k config/samples/persistence/
```

This creates a PostgreSQL instance, a 3-replica runtime with lease-based sharding, and a `sleepy-workflow` (30s wait task). Test durable recovery:

```sh
# Fire a workflow
curl -X POST http://localhost:8080/q/flow/exec/default/sleepy-workflow/1.0.0 \
  -H "Content-Type: application/json" -d '{"name": "durable-test"}'

# Kill the pod — the workflow resumes on another replica
kubectl delete pod -l app.kubernetes.io/name=hello-runtime --wait=false
```

### Enable Monitoring (Prometheus + Grafana)

Deploy Prometheus and Grafana with a pre-configured "Logic Flow Runtime" dashboard:

```sh
kubectl apply -k config/samples/monitoring/
```

- **Grafana**: http://grafana.lvh.me (anonymous access, no login required)
- **Prometheus**: http://prometheus.lvh.me

Open Grafana and navigate to **Dashboards > Logic Operator > Logic Flow Runtime**. The dashboard shows workflow start/completion rates, active instance gauges, task duration histograms, and per-pod distribution. Metrics appear after the first workflow execution.

To clean up:

```sh
make kind-undemo    # remove sample CRs
kubectl delete -k config/samples/monitoring/ --ignore-not-found=true
kubectl delete -k config/samples/persistence/ --ignore-not-found=true
make kind-delete    # delete the KIND cluster
```

### Available Make targets

| Target | Description |
|--------|-------------|
| `make kind-create` | Create KIND cluster with ingress and cert-manager |
| `make kind-deploy` | Build image, load into KIND, deploy operator |
| `make kind-demo` | Apply sample CRs |
| `make kind-undemo` | Remove sample CRs |
| `make kind-delete` | Delete the KIND cluster |
| `make run` | Run the operator out-of-cluster (day-to-day development) |
| `make test` | Run unit and integration tests |
| `make lint` | Run golangci-lint |
| `make setup-test-e2e` | Set up E2E test infrastructure (cluster, cert-manager, operator) |
| `make test-e2e` | Run end-to-end tests (requires setup-test-e2e first) |
| `make cleanup-test-e2e` | Delete the E2E test cluster |

Run `make help` for the full list.

### Deploy to an existing cluster

If you already have a Kubernetes cluster with cert-manager installed:

```sh
make install                                        # install CRDs
make deploy IMG=<some-registry>/logic-operator:tag  # deploy the operator
kubectl apply -k config/samples/                    # apply sample CRs
```

To uninstall:

```sh
kubectl delete -k config/samples/
make undeploy
make uninstall
```

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes following the existing code style
4. Run tests: `make test`
5. Run linter: `make lint`
6. Submit a pull request

For major changes, please open an issue first to discuss what you would like to change

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

