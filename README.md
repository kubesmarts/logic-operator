<!--
   Licensed to the Apache Software Foundation (ASF) under one
   or more contributor license agreements.  See the NOTICE file
   distributed with this work for additional information
   regarding copyright ownership.  The ASF licenses this file
   to you under the Apache License, Version 2.0 (the
   "License"); you may not use this file except in compliance
   with the License.  You may obtain a copy of the License at
     http://www.apache.org/licenses/LICENSE-2.0
   Unless required by applicable law or agreed to in writing,
   software distributed under the License is distributed on an
   "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
   KIND, either express or implied.  See the License for the
   specific language governing permissions and limitations
   under the License.
-->

# Logic Operator

The Logic Operator is a Kubernetes operator for deploying and managing serverless workflow applications.
It defines a set of [Kubernetes Custom Resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
to help users deploy workflow projects on Kubernetes and OpenShift.

This operator is a continuation of the Apache KIE SonataFlow Operator, maintained by Kubesmarts.

## Documentation

For detailed documentation, please visit [the official documentation](https://kiegroup.github.io/kogito-docs/serverlessworkflow/latest/cloud/operator/install-serverless-operator.html).

## Available modules for integrations

If you're a developer, and you are interested in integrating your project or application with the SonataFlow Operator
ecosystem, this repository provides a few Go Modules described below.

### SonataFlow Operator Types (api)

Every custom resource managed by the operator is exported in the module [api](api). You can use it to programmatically
create any custom type managed by the operator.
To use it, simply run:

```shell
go get github.com/kubesmarts/logic-operator/api
```

Then you can create any type programmatically, for example:

```go
import (
    "github.com/kubesmarts/logic-operator/api/v1alpha08"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

workflow := &v1alpha08.SonataFlow{
    ObjectMeta: metav1.ObjectMeta{Name: w.name, Namespace: w.namespace},
    Spec:       v1alpha08.SonataFlowSpec{Flow: *myWorkflowDef},
}
```

You can use the [Kubernetes client-go library](https://github.com/kubernetes/client-go) to manipulate these objects in
the cluster.

You might need to register our schemes:

```go
    s := scheme.Scheme
utilruntime.Must(v1alpha08.AddToScheme(s))
```

### Container Builder (container-builder)

Please see the module's [README file](container-builder/README.md).

### Workflow Project Handler (workflowproj)

Please see the module's [README file](workflowproj/README.md).

## Build Requirements

To build and develop the Logic Operator, you need:

- **Go** 1.25.0 or later
- **Make**
- **Python 3** with `ruamel.yaml` package
- **Node.js** (for `replace-in-file` utility used in version bumping)
- **Docker** or **Podman** (for container builds)
- Optional: **kubectl**, **kind** (for local testing)

### Configuration

The operator uses a `.env` file for build configuration. Copy the example file:

```bash
cp .env.example .env
```

Edit `.env` to customize version numbers, registry locations, and image references.
All values can be overridden by environment variables.

## Development and Contributions

Contributing is easy, just take a look at our [contributors](https://github.com/kubesmarts/logic-operator/blob/main/docs/CONTRIBUTING.md) guide.
