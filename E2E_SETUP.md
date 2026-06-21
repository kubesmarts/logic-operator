# E2E Test Setup for Logic Operator

## Overview

The Logic Operator e2e tests verify the complete operator functionality including workflow builds, deployments, and integrations with Knative and Prometheus.

## Prerequisites

### Required Tools
- `kubectl`
- `kind` (v0.20.0+) OR `minikube`
- `docker` or `podman`
- `go` (1.26.0+)
- `python3` with `cekit` (see hack/setup-cekit.sh)
- `make`

### System Dependencies (for CEKit)
Ubuntu/Debian:
```bash
sudo apt-get install libxml2-dev libxslt-dev python3-dev libkrb5-dev
```

macOS:
```bash
brew install libxml2 libxslt krb5
```

Fedora/RHEL:
```bash
sudo dnf install libxml2-devel libxslt-devel python3-devel krb5-devel
```

### Required Images
All images are hosted at `quay.io/kubesmarts` with tag `main`:
- `incubator-kie-sonataflow-builder:main`
- `incubator-kie-sonataflow-devmode:main`
- `incubator-kie-kogito-jobs-service-postgresql:main`
- `incubator-kie-kogito-jobs-service-ephemeral:main`
- `incubator-kie-kogito-data-index-postgresql:main`
- `incubator-kie-kogito-data-index-ephemeral:main`
- `incubator-kie-kogito-db-migrator:main`

## Quick Start (KIND)

The full e2e suite using KIND:

```bash
# 1. Setup CEKit (one time)
./hack/setup-cekit.sh
source venv/bin/activate

# 2. Pull test images
./hack/pull-test-images.sh

# 3. Run full e2e suite
make full-test-e2e
```

This will:
1. Create a KIND cluster with local registry
2. Build and load the operator image
3. Load builder and devmode images
4. Deploy the operator
5. Install Knative Serving and Eventing
6. Install Prometheus
7. Run all e2e test suites:
   - Platform tests
   - Cluster platform tests  
   - Workflow monitoring tests
   - Workflow persistence tests
   - Workflow ephemeral tests

## Alternative: Minikube

For local development with Minikube:

```bash
./hack/local/run-e2e.sh [profile] [skip-build] [test-label] [skip-undeploy]
```

Examples:
```bash
# Run platform tests with fresh build
./hack/local/run-e2e.sh minikube false platform false

# Run ephemeral tests, skip build
./hack/local/run-e2e.sh minikube true flows-ephemeral false
```

## Test Labels

- `platform` - SonataFlowPlatform deployment and configuration tests
- `cluster` - SonataFlowClusterPlatform tests
- `flows-ephemeral` - Workflow tests without persistence
- `flows-persistence` - Workflow tests with database persistence  
- `flows-monitoring` - Workflow monitoring with Prometheus
- `flows-hpa` - Horizontal Pod Autoscaler tests
- `flows-pdb` - Pod Disruption Budget tests
- `flows-pdb-with-hpa` - Combined PDB and HPA tests

## CI/CD Setup

The repository includes GitHub Actions workflows in `.github/workflows/`:

### E2E Tests Workflow (`.github/workflows/e2e.yaml`)

Runs the full e2e test suite on every PR and push to main/master:

1. Sets up Go 1.25 and Python
2. Installs CEKit in a Python venv
3. Pulls test images from quay.io/kubesmarts
4. Builds operator container image
5. Creates KIND cluster with local registry
6. Loads images into KIND
7. Deploys operator, Knative, and Prometheus
8. Runs all e2e test suites:
   - platform
   - cluster
   - flows-monitoring
   - flows-persistence
   - flows-ephemeral
9. Collects logs and test results on failure
10. Uploads test artifacts

**Trigger:** Pull requests and pushes to main/master, or manual via `workflow_dispatch`

### PR Checks Workflow (`.github/workflows/pr-checks.yaml`)

Fast checks for pull requests:

1. **lint-and-test job:**
   - Go fmt check
   - Go vet
   - Unit tests
   - Build operator binary
   - Verify Go modules are tidy

2. **verify-manifests job:**
   - Verify generated manifests are up to date
   - Verify CRD bundle is up to date

**Trigger:** Pull requests only

These workflows use the existing Makefile targets, so local development and CI use the same build/test commands

### Faster CI Option: Unit Tests Only

For quick PR checks, run only unit tests:

```yaml
- name: Run unit tests
  run: make test
```

## Build System

The operator uses **CEKit** (Container Evolution Kit) for building container images. CEKit must be installed in a Python virtual environment:

```bash
# One-time setup
python3 -m venv venv
source venv/bin/activate
pip install -r images/requirements.txt
```

Add `venv/bin/activate` sourcing to your build scripts.

## Workflow Builds in Tests

The e2e tests use **Kaniko** to build workflow containers inside the cluster. The BeforeSuite:

1. Creates `e2e-resources` namespace
2. Deploys sample workflows
3. Creates a SonataFlowPlatform with Kaniko configuration
4. Waits for workflows to build into container images
5. Extracts built image tags
6. Scales workflows to 0 (images cached in local registry)
7. Individual tests deploy workflows using pre-built images

This requires:
- A container registry accessible from the cluster (KIND local registry or Minikube registry addon)
- Builder images loaded into the cluster
- Kaniko build cache enabled in the platform

## Troubleshooting

### CEKit not found
```bash
source venv/bin/activate
```

### Image pull failures
Check that `.env` file exists with correct image references:
```bash
cp .env.example .env
```

### KIND kube-proxy errors
Usually safe to ignore if nodes show Ready. May indicate local file descriptor limits.

### Workflow builds not starting
Ensure Son ataFlowPlatform exists in the test namespace with Kaniko configuration:
```yaml
spec:
  build:
    config:
      strategyOptions:
        KanikoBuildCacheEnabled: "true"
```

## Clean Up

```bash
# Delete KIND cluster
make delete-cluster

# Clean test namespaces
kubectl delete namespace e2e-resources
kubectl get namespaces | grep test- | awk '{print $1}' | xargs kubectl delete namespace
```
