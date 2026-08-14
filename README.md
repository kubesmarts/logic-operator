# Logic Operator

A Kubernetes operator for managing Serverless Logic workflows powered by Quarkus Flow.

## Description

The Logic Operator provides a Kubernetes-native way to deploy, manage, and scale Serverless Logic workflows. Built on top of Quarkus Flow, it offers four core CRDs for defining and orchestrating workflow-based applications:

- **LogicPlatform**: Platform-wide configuration and shared services
- **LogicFlowService**: Application definitions for workflow deployments
- **LogicFlowDefinition**: Workflow definitions and specifications
- **LogicFlowRuntime**: Runtime instances of deployed workflows

This is v2.0 of the operator, representing a complete architectural overhaul with modern Kubernetes patterns and improved scalability

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
make kind-demo      # apply sample CRs (Runtime + Definition + Service)
```

Check that everything is running:

```sh
kubectl get logicflowruntimes,logicflowdefinitions,logicflowservices
```

The sample deploys a `hello-world` workflow you can invoke once the runtime pod is ready:

```sh
kubectl wait --for=condition=available deployment/hello-runtime --timeout=120s
curl -X POST http://hello.lvh.me/ \
  -H "Content-Type: application/json" \
  -d ‘{"name": "World"}’
```

> `lvh.me` resolves to `127.0.0.1` — no `/etc/hosts` editing needed.

The runtime is also exposed directly at `http://runtime.lvh.me/` for accessing OpenAPI specs, health checks, and dashboards:

```sh
curl http://runtime.lvh.me/q/openapi   # OpenAPI spec
curl http://runtime.lvh.me/q/health    # health checks
```

Alternatively, you can port-forward the runtime service without ingress:

```sh
kubectl port-forward svc/hello-runtime 8080:80
curl http://localhost:8080/q/openapi
```

To clean up:

```sh
make kind-undemo    # remove sample CRs
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
| `make test-e2e` | Run end-to-end tests (creates its own cluster) |

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

