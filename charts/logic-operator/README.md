# Logic Operator chart source

This directory contains the hand-maintained source for the `logic-operator` Helm chart. In the repository checkout, its Kubernetes resource templates are generated during packaging from `config/default`; the CRD bundle is generated from `config/crd` and kept separate from the chart.

Do not install this source directory directly. Build the release artifacts from the repository root:

```sh
make helm-package OPERATOR_VERSION=2.0.0-Alpha3
```

This stages the generated chart resources and packages both charts under `dist/helm/`. It also writes the standalone `dist/helm/crds.yaml` release asset. The generated resource templates and CRDs are not committed under `charts/`.

Install the published OCI chart for released deployments. Apply the matching release's `crds.yaml` with server-side apply before the first Helm install and before each chart upgrade. See the [Helm installation guide](../../docs/antora/modules/ROOT/pages/deployment/helm.adoc).
