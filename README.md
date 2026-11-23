# Logic Operator

A Kubernetes operator for deploying and managing [Open Workflow Specification](https://open-workflow-specification.org/) workflows powered by [Quarkus Flow](https://docs.quarkiverse.io/quarkus-flow/dev/index.html).

You describe your workflows and the runtimes that execute them as Kubernetes resources. The operator deploys the Quarkus Flow runners, an optional Workflow Gateway to invoke them, and optional Data Index and Vector components to persist and query workflow history.

| Resource | Purpose |
|----------|---------|
| `ClusterLogicPlatform` | Cluster-wide singleton that deploys the Workflow Gateway, and optionally Data Index and Vector |
| `LogicFlowRuntime` | A Quarkus Flow Runner Deployment that executes workflows |
| `LogicFlowDefinition` | A workflow document, loaded by the runtime it references |
| `LogicFlowService` | Optional external endpoint and traffic-split policy for a workflow |

Full documentation: **https://kubesmarts.org/logic-operator/**

## 1. Install the operator

This installs only the operator and its CRDs from a published release, on a Kubernetes or OpenShift cluster that you manage. Your first workflow comes in [section 3](#3-run-your-first-workflow).

Prerequisites:

- Kubernetes 1.29+ or OpenShift, with a working `kubectl` context
- [Helm](https://helm.sh/docs/intro/install/) 3.8+
- cert-manager, used for the operator's webhook certificates

Check that `kubectl` points at the cluster you intend to change:

```sh
kubectl config current-context
```

If cert-manager is not installed yet:

```sh
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
kubectl wait --namespace cert-manager \
  --for=condition=available deployment --all --timeout=180s
kubectl wait --for=condition=Established --timeout=60s \
  crd/certificates.cert-manager.io crd/issuers.cert-manager.io
```

Pick the release to install. The Helm chart does not contain the CRDs, so apply the release's `crds.yaml` first:

```sh
VERSION=2.0.0-Alpha4
curl -fsSL "https://github.com/kubesmarts/logic-operator/releases/download/v${VERSION}/crds.yaml" \
  | kubectl apply --server-side -f -
```

Install the operator:

```sh
helm install logic-operator \
  oci://ghcr.io/kubesmarts/charts/logic-operator \
  --version "$VERSION" \
  --namespace logic-operator-system \
  --create-namespace
```

Check that it is running:

```sh
kubectl wait --namespace logic-operator-system \
  --for=condition=available deployment --all --timeout=120s
kubectl get crd | grep logic.kubesmarts.org
```

For every upgrade, apply the target release's `crds.yaml` with server-side apply before running `helm upgrade`. See the [Helm installation and upgrade guide](docs/antora/modules/ROOT/pages/deployment/helm.adoc) for upgrades, image overrides, and the `logic-operator-stack` chart, which installs the operator, PostgreSQL, and a sample platform in one step.

## 2. Next step: install PostgreSQL

Durable workflows, workflow history, and Data Index need a PostgreSQL database. Skip this step if you only want to try an ephemeral workflow.

The repository ships a **disposable, single-pod sample** at [`config/samples/persistence/postgresql.yaml`](config/samples/persistence/postgresql.yaml). It creates a Secret (`flowuser` / `flowpass`), a 1Gi PVC, a `postgres:16-alpine` Deployment, a Service named `postgresql`, and the `runtime`, `quartz`, and `data_index` schemas in a `logicflow` database.

The platform and PostgreSQL share the `logic-platform` namespace in the samples, so create it first:

```sh
kubectl create namespace logic-platform
kubectl apply -n logic-platform \
  -f "https://raw.githubusercontent.com/kubesmarts/logic-operator/v${VERSION}/config/samples/persistence/postgresql.yaml"
kubectl wait -n logic-platform --for=condition=available deployment/postgresql --timeout=120s
```

The sample uses a demo password and a default StorageClass. For anything beyond a test cluster, use your own managed PostgreSQL or database operator and point the `ClusterLogicPlatform` at it, following the [CRD reference](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/crd-reference/clusterlogicplatform.html).

## 3. Run your first workflow

From here, follow the [documentation](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/index.html). Create the resources in this order, because each one refers to the previous:

| Order | Resource | What it does | Reference |
|-------|----------|--------------|-----------|
| 1 | `ClusterLogicPlatform` | Deploys the Workflow Gateway (and Data Index and Vector if configured) into its `targetNamespace`. There is one per cluster, and the namespace must already exist. | [ClusterLogicPlatform](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/crd-reference/clusterlogicplatform.html) |
| 2 | `LogicFlowRuntime` | Runs the Quarkus Flow Runner as a Deployment and Service. `persistenceMode: Ephemeral` needs no database. `PlatformDB` uses the PostgreSQL from section 2 through `platformRef`. | [LogicFlowRuntime](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/crd-reference/logicflowruntime.html) |
| 3 | `LogicFlowDefinition` | Holds the workflow document and points to its runtime with `runtimeRef`. The operator loads it into the runtime. | [LogicFlowDefinition](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/crd-reference/logicflowdefinition.html) |
| 4 | Invoke it | Call the Workflow Gateway at `/v1/runtime/{namespace}/{name}/{version}/sync`. The namespace is the one the `LogicFlowDefinition` is created in. | [Workflow Gateway](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/components/workflow-gateway.html) |
| 5 | `LogicFlowService` (optional) | Adds an external endpoint and traffic splitting between workflow versions. | [LogicFlowService](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/crd-reference/logicflowservice.html) |

Two guided paths use exactly these resources:

- **[Hello World](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/samples/hello-world.html)**: an ephemeral runtime and a one-step workflow, with no database. Start here.
- **[Durable workflow with PostgreSQL](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/samples/persistence.html)**: builds on section 2 to add durable state, workflow history, and Data Index GraphQL queries.

### Reaching the Workflow Gateway

The gateway is a ClusterIP Service (`<platform-name>-workflow-gateway`, port 80) in the platform's `targetNamespace`. To call it from outside the cluster, expose it in one of these ways:

- **Ingress or Route.** Set `spec.gateway.ingress` on the `ClusterLogicPlatform` with `enabled: true` and a host. **The operator does not install an Ingress controller**, so the cluster needs one. On OpenShift the operator creates a Route from an apps-domain host. See [Expose the Workflow Gateway](docs/antora/modules/ROOT/pages/deployment/helm.adoc#expose-the-workflow-gateway).
- **Port forwarding.** This needs no Ingress and works on any cluster, which makes it the quickest way to try things:

  ```sh
  kubectl port-forward -n logic-platform svc/default-workflow-gateway 8080:80
  curl -X POST http://localhost:8080/v1/runtime/default/hello-world/1.0.0/sync \
    -H "Content-Type: application/json" -d '{"name": "World"}'
  ```

  Use your platform's `targetNamespace` and name in place of `logic-platform` and `default`.

> **Running on KIND?** The sample manifests in [`config/samples/`](config/samples/) expose the gateway at `gateway.lvh.me` through an Ingress, so **KIND needs the NGINX Ingress controller** (ingress-nginx) with ports 80 and 443 mapped to your machine. A plain `kind create cluster` has neither, and the sample URLs will not respond. Use `make kind-create`, which installs ingress-nginx and applies the port mapping from [`hack/kind-config.yaml`](hack/kind-config.yaml). Otherwise, install ingress-nginx yourself using its [KIND provider manifest](https://kubernetes.github.io/ingress-nginx/deploy/#kind) on a cluster created with those port mappings, or use port forwarding as above. `*.lvh.me` resolves to `127.0.0.1`, so no `/etc/hosts` edits are needed.

Other useful pages: [Runtime Model](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/concepts/runtime-model.html), [Durable Workflows](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/concepts/durable-workflows.html), [Monitoring](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/deployment/monitoring.html), and [Troubleshooting](https://kubesmarts.org/logic-operator/logic-operator/2.0.0-Alpha4/deployment/troubleshooting.html).

## 4. Development with KIND

The `make kind-*` recipes are for developers and contributors working from a local clone. They build and apply the **current checkout**, including the selected branch and uncommitted changes. They do not install a published release. Run them from the repository root.

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

A new cluster gets ingress-nginx, cert-manager, and the Quarkus Flow runner images. **ingress-nginx is what exposes the gateway at `gateway.lvh.me`**, together with the port 80/443 mappings in `hack/kind-config.yaml`. If you use your own KIND cluster without it, reach the gateway with `kubectl port-forward` instead (see [Reaching the Workflow Gateway](#reaching-the-workflow-gateway)). If the KIND cluster already exists, `kind-create` skips cluster and add-on setup. See the [KIND developer guide](docs/antora/modules/ROOT/pages/deployment/kind-local.adoc) for all targets and troubleshooting.

`make kind-demo` applies [`config/samples/`](config/samples/): a `ClusterLogicPlatform` (gateway in the `logic-platform` namespace), an ephemeral `LogicFlowRuntime` named `hello-runtime`, and the hello-world `LogicFlowDefinition`, both in `default`. Check the resources and invoke the workflow:

```sh
kubectl get clusterlogicplatforms,logicflowruntimes,logicflowdefinitions
kubectl wait --for=condition=available deployment/hello-runtime --timeout=120s
kubectl wait -n logic-platform --for=condition=available deployment/default-workflow-gateway --timeout=120s
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

Enable persistence and a durable workflow. This applies the PostgreSQL sample from section 2 together with a platform and runtime that use it, and it must target the `logic-platform` namespace:

```sh
kubectl apply -k config/samples/persistence/ -n logic-platform
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
kubectl delete -k config/samples/persistence/ -n logic-platform --ignore-not-found=true
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

Run `make help` from the repository root for the complete target list. Operator and component versions are declared in [`versions.mk`](versions.mk).

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

