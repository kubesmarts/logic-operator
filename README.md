# Logic Operator

A Kubernetes operator for managing Serverless Logic workflows powered by Quarkus Flow.

## Description

The Logic Operator provides a Kubernetes-native way to deploy, manage, and scale Serverless Logic workflows. Built on top of Quarkus Flow, it offers four core CRDs for defining and orchestrating workflow-based applications:

- **ClusterLogicPlatform**: Data Index, optional Vector, and optional Workflow Gateway management
- **LogicFlowService**: External workflow endpoint and optional traffic-split policy
- **LogicFlowDefinition**: Workflow definitions and specifications
- **LogicFlowRuntime**: Runtime instances of deployed workflows

This repository tracks the 2.0 development line. Operator and component versions are declared in `versions.mk`.

The standalone Workflow Gateway accepts execution and lifecycle requests and can route directly
to a workflow's runtime Service. See the [Workflow Gateway guide](docs/antora/modules/ROOT/pages/components/workflow-gateway.adoc) for its API, ClusterLogicPlatform integration, and current scaling limitations.

## Getting Started

For a released version, install the Helm chart into a Kubernetes or OpenShift cluster that you manage. To build and exercise source code from a local checkout, follow the [KIND developer quickstart](#local-development-with-kind).

### Install a released stack

The `logic-operator-stack` chart installs the operator, PostgreSQL, and a `ClusterLogicPlatform` with Data Index, Vector, and Workflow Gateway. You remain responsible for creating and managing the cluster.

Prerequisites:

- Kubernetes or OpenShift cluster with a working `kubectl` context
- Helm 3.8+ and kubectl
- cert-manager installed before the operator
- For persistent PostgreSQL storage, a default StorageClass or an explicitly configured one

Check the current context before installing:

```sh
kubectl config current-context
```

If cert-manager is not already installed, install the same release used by the repository's `make kind-create` recipe:

```sh
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
kubectl wait --namespace cert-manager \
  --for=condition=available deployment --all --timeout=180s
kubectl wait --for=condition=Established --timeout=60s \
  crd/certificates.cert-manager.io crd/issuers.cert-manager.io
```

Set `VERSION` to the release you want to install. The following example uses `2.0.0-Alpha3`. Apply the matching CRDs before installing either chart:

```sh
VERSION=2.0.0-Alpha3
curl -fsSL "https://github.com/kubesmarts/logic-operator/releases/download/v${VERSION}/crds.yaml" \
  | kubectl apply --server-side -f -
```

Then install the stack:

```sh
helm install logic-stack \
  oci://ghcr.io/kubesmarts/charts/logic-operator-stack \
  --version "$VERSION" \
  --namespace logic-operator-system \
  --create-namespace
```

The chart installs the database and platform components; it does not create workflow definitions or runtimes. Configure PostgreSQL credentials and other chart settings in a values file before using the stack beyond a disposable cluster. See the [Helm installation and upgrade guide](docs/antora/modules/ROOT/pages/deployment/helm.adoc).

If you only need the operator and CRDs, install the smaller chart instead:

```sh
helm install logic-operator \
  oci://ghcr.io/kubesmarts/charts/logic-operator \
  --version "$VERSION" \
  --namespace logic-operator-system \
  --create-namespace
```

The release's *Publish Images* workflow must have completed before installing, so the referenced Quay images exist. The GHCR chart packages must be public for anonymous pulls. The charts exclude CRDs. For every upgrade, apply the target release's `crds.yaml` with server-side apply before running `helm upgrade`; see the [upgrade instructions](docs/antora/modules/ROOT/pages/deployment/helm.adoc#upgrade).

### Expose the Workflow Gateway

Gateway ingress is disabled by default. Enable it with a hostname and, for Kubernetes, an IngressClass that exists on your cluster.

For a local KIND cluster created by `make kind-create`, use the host from the sample:

```yaml
clusterLogicPlatform:
  gateway:
    ingress:
      enabled: true
      host: gateway.lvh.me
      className: nginx
```

`gateway.lvh.me` resolves to localhost and is only suitable for a local cluster configured with ingress-nginx. On OpenShift, the operator detects the platform and creates a Route when ingress is enabled. Provide a host from your cluster's apps domain, for example `gateway.apps.example.com`, and omit `className`. For other Kubernetes clusters, provide a DNS name that resolves to the Ingress controller. The operator does not install an Ingress controller or OpenShift router. More examples are in the [Helm guide](docs/antora/modules/ROOT/pages/deployment/helm.adoc#expose-the-workflow-gateway).

## Local Development with KIND

The `make kind-*` recipes are for developers and contributors working from a local clone. They build and apply the current checkout, including the currently selected branch and uncommitted changes; they do not install a published release. Run them from the repository root and choose the branch or commit you want to test first.

Clone the repository and select the branch or commit you want to work on:

```sh
git clone https://github.com/kubesmarts/logic-operator.git
cd logic-operator
# git switch <branch>
```

Prerequisites:

- [Go](https://go.dev/dl/) v1.26+
- [Docker](https://docs.docker.com/get-docker/) v17.03+
- [kubectl](https://kubernetes.io/docs/tasks/tools/) v1.30+
- [KIND](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) v0.20+

Create a local cluster, deploy the operator from source, and apply the hello-world sample:

```sh
make kind-create    # create KIND and install ingress-nginx + cert-manager
make kind-deploy    # build and deploy the operator from this checkout
make kind-demo      # build/load the gateway image and apply sample resources
```

A newly created cluster gets ingress-nginx and cert-manager. If the KIND cluster already exists, `kind-create` skips cluster and addon installation. See the [KIND developer guide](docs/antora/modules/ROOT/pages/deployment/kind-local.adoc) for all targets and troubleshooting.

Check that the sample resources were created and invoke the workflow:

```sh
kubectl get clusterlogicplatforms,logicflowruntimes,logicflowdefinitions
kubectl wait --for=condition=available deployment/hello-runtime --timeout=120s
kubectl wait --for=condition=available deployment/default-workflow-gateway --timeout=120s
curl -X POST http://gateway.lvh.me/v1/runtime/default/hello-world/1.0.0/sync \
  -H "Content-Type: application/json" \
  -d '{"name": "World"}'
```

Expected response:

```json
{"message": "Hi, World!"}
```

`lvh.me` resolves to `127.0.0.1`, so no `/etc/hosts` editing is needed with the repository's KIND ingress configuration.

### Explore the local samples

To inspect the runtime's OpenAPI spec or health checks, start this port-forward in a separate terminal:

```sh
kubectl port-forward svc/hello-runtime 8080:80
curl http://localhost:8080/q/openapi   # OpenAPI spec
curl http://localhost:8080/q/health    # health checks
```

Enable API key authentication:

```sh
kubectl apply -k config/samples/authentication/
kubectl rollout restart deployment/hello-runtime
kubectl rollout status deployment/hello-runtime --timeout=120s
```

Enable persistence and a durable workflow:

```sh
kubectl apply -k config/samples/persistence/
```

Enable Prometheus and Grafana:

```sh
kubectl apply -k config/samples/monitoring/
```

The local monitoring sample exposes Grafana at `http://grafana.lvh.me` and Prometheus at `http://prometheus.lvh.me`.

Clean up the local demo and cluster:

```sh
make kind-undemo
kubectl delete -k config/samples/monitoring/ --ignore-not-found=true
kubectl delete -k config/samples/persistence/ --ignore-not-found=true
make kind-delete
```

### Useful Make targets

| Target | Description |
|--------|-------------|
| `make kind-create` | Create KIND and install ingress-nginx and cert-manager |
| `make kind-deploy` | Build from the current checkout, load into KIND, and deploy |
| `make kind-build-gateway` | Build and load the gateway image |
| `make kind-demo` | Build/load the gateway and apply sample CRs |
| `make kind-undemo` | Remove sample CRs |
| `make kind-delete` | Delete the local KIND cluster |
| `make run` | Run the operator out of cluster for development |
| `make test` | Run unit and integration tests |
| `make lint` | Run golangci-lint |
| `make setup-test-e2e` | Set up E2E test infrastructure |
| `make test-e2e` | Run end-to-end tests |
| `make cleanup-test-e2e` | Delete the E2E test cluster |

Run `make help` from the repository root for the complete target list.

### Deploy a source build to an existing cluster

This is an advanced contributor workflow for deploying the local source build to the current `kubectl` context. It is not the recommended way to install a release. Confirm the context before applying changes:

```sh
kubectl config current-context
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

