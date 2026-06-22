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

# kn-workflow CLI

`kn-workflow` is a command-line tool for creating, managing, and deploying SonataFlow projects. It enables users to quickly set up local SonataFlow projects and deploy them to Kubernetes via the Logic Operator.

This CLI is part of the [Logic Operator](https://github.com/kubesmarts/logic-operator) project.

[Read the documentation](https://sonataflow.org/serverlessworkflow/main/testing-and-troubleshooting/kn-plugin-workflow-overview.html)

## Build from source

### Prerequisites

- Go `1.26.0+` _(To install, follow these instructions: https://go.dev/doc/install)_
- Make

### Building

To build the CLI for your current platform:

```bash
make build
```

The binary will be created at `./dist/kn-workflow`.

### Building for all platforms

To build the CLI for all supported platforms (Linux, macOS, Windows):

```bash
make build-all
```

Artifacts are generated in the `dist/` directory:
- `kn-workflow-darwin-amd64` - macOS Intel
- `kn-workflow-darwin-arm64` - macOS Apple Silicon
- `kn-workflow-linux-amd64` - Linux
- `kn-workflow-windows-amd64.exe` - Windows

### Configuration

Build metadata (Quarkus version, container images) is inherited from the parent `.env` file. You can override these values by setting environment variables:

```bash
QUARKUS_VERSION=3.8.1 make build
```

### Running tests

Run unit tests:

```bash
make test
```

Run end-to-end tests (requires Docker/Podman and KIND):

```bash
make test-e2e
```

### Clean build artifacts

```bash
make clean
```

## Installation

After building, copy the binary to your PATH:

```bash
# macOS/Linux
sudo cp dist/kn-workflow /usr/local/bin/

# Or install to user bin
cp dist/kn-workflow ~/.local/bin/
```

## Usage

```bash
# Create a new workflow project
kn-workflow create --name my-workflow

# Deploy to Kubernetes
kn-workflow deploy --namespace my-namespace

# Run in development mode
kn-workflow run

# Show version
kn-workflow version
```

For more commands and options, run:

```bash
kn-workflow --help
```

---

Apache KIE (incubating) is an effort undergoing incubation at The Apache Software
Foundation (ASF), sponsored by the name of Apache Incubator. Incubation is
required of all newly accepted projects until a further review indicates that
the infrastructure, communications, and decision making process have stabilized
in a manner consistent with other successful ASF projects. While incubation
status is not necessarily a reflection of the completeness or stability of the
code, it does indicate that the project has yet to be fully endorsed by the ASF.

Some of the incubating project's releases may not be fully compliant with ASF
policy. For example, releases may have incomplete or un-reviewed licensing
conditions. What follows is a list of known issues the project is currently
aware of (note that this list, by definition, is likely to be incomplete):

- Hibernate, an LGPL project, is being used. Hibernate is in the process of
  relicensing to ASL v2
- Some files, particularly test files, and those not supporting comments, may
  be missing the ASF Licensing Header

If you are planning to incorporate this work into your product/project, please
be aware that you will need to conduct a thorough licensing review to determine
the overall implications of including this work. For the current status of this
project through the Apache Incubator visit:
https://incubator.apache.org/projects/kie.html
