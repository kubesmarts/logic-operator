# Logic Operator Stack chart source

This directory contains the hand-maintained source for the `logic-operator-stack` chart. Its local dependency on `logic-operator` is resolved during packaging; the generated operator templates are staged from `config/default` and are not committed under `charts/`.

Do not install this source directory directly. Build the release artifacts from the repository root:

```sh
make helm-package OPERATOR_VERSION=2.0.0-Alpha3
```

The command packages the operator and stack charts under `dist/helm/` and writes `dist/helm/crds.yaml` separately. The charts do not include CRDs. Apply the matching release's CRD bundle with server-side apply before the first Helm install and before every chart upgrade. See the [Helm installation guide](../../docs/antora/modules/ROOT/pages/deployment/helm.adoc).
