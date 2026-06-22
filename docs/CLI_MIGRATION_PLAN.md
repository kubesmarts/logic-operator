# CLI Migration Plan: kn-plugin-workflow → logic-operator/cli

**Status:** PLANNING - Execute AFTER CI is stable
**Estimated Effort:** 3-5 days
**Complexity:** MEDIUM

## Overview

Migrate `kn-plugin-workflow` from incubator-kie-tools monorepo to `logic-operator` repo as `cli/` subdirectory.

**Current Location:** `/packages/kn-plugin-workflow` in incubator-kie-tools  
**Target Location:** `/cli` in logic-operator  
**Binary Name:** `kn-workflow` (unchanged)

## Package Summary

- **Size:** 75 Go files (~4,789 LOC)
- **Commands:** 9 top-level commands (create, run, deploy, undeploy, gen-manifest, quarkus, version, specs, operator)
- **Framework:** Cobra-based CLI
- **Build:** Go 1.25.0 + Makefile (currently wrapped by pnpm)
- **Tests:** 13 e2e test files

## Key Dependencies

### Internal (incubator-kie-tools)
- `sonataflow-operator/api` ✅ Already in logic-operator
- `sonataflow-operator/workflowproj` ✅ Already in logic-operator  
- Image packages (builder, devmode) → Need new defaults

### External (Go modules)
- k8s.io libraries (client-go, api, apimachinery)
- github.com/spf13/cobra
- github.com/docker/docker
- github.com/serverlessworkflow/sdk-go/v2

## Migration Phases

### Phase 1: Preparation (1 day)
- [ ] Ensure CI is stable and green
- [ ] Create `cli/` directory structure
- [ ] Document current metadata values (Quarkus versions, image URLs)
- [ ] Decide on CLI versioning strategy (separate vs coupled with operator)

### Phase 2: Code Migration (1 day)
- [ ] Copy entire kn-plugin-workflow structure to `cli/`
- [ ] Create `cli/go.mod` with new module path
- [ ] Update all import paths (~75 files):
  ```
  OLD: github.com/apache/incubator-kie-tools/packages/kn-plugin-workflow
  NEW: github.com/kubesmarts/logic-operator/cli
  
  OLD: github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api
  NEW: github.com/kubesmarts/logic-operator/api
  
  OLD: github.com/apache/incubator-kie-tools/packages/sonataflow-operator/workflowproj
  NEW: github.com/kubesmarts/logic-operator/workflowproj
  ```
- [ ] Add `cli/` to `go.work`

### Phase 3: Build System Refactoring (1 day)
- [ ] Remove pnpm/Node.js wrapper (delete `package.json`, `env/`)
- [ ] Simplify Makefile:
  - Replace build-env system with environment variables
  - Hardcode sensible defaults
  - Support override via `.env` file
- [ ] Update metadata injection approach:
  ```makefile
  VERSION ?= $(shell git describe --tags --always)
  QUARKUS_VERSION ?= 3.8.1
  BUILDER_IMAGE ?= quay.io/kubesmarts/incubator-kie-sonataflow-builder:main
  DEVMODE_IMAGE ?= quay.io/kubesmarts/incubator-kie-sonataflow-devmode:main
  
  LDFLAGS = -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.PluginVersion=$(VERSION) \
            -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.QuarkusVersion=$(QUARKUS_VERSION) \
            -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.BuilderImage=$(BUILDER_IMAGE) \
            -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.DevModeImage=$(DEVMODE_IMAGE)
  ```

### Phase 4: Testing & Validation (1 day)
- [ ] Local build test: `cd cli && make build`
- [ ] Run unit tests: `go test ./...`
- [ ] Update e2e tests for new paths
- [ ] Verify operator integration
- [ ] Test all 9 commands smoke tests

### Phase 5: CI/CD Integration (1 day)
- [ ] Add CLI build to `.github/workflows/`
- [ ] Multi-platform builds (darwin-amd64, darwin-arm64, linux-amd64, windows-amd64)
- [ ] Upload binaries as GitHub release artifacts
- [ ] Update README with new build instructions

## Files to Keep
- ✅ All `.go` files (with updated imports)
- ✅ `Makefile` (simplified)
- ✅ `go.mod`, `go.sum` (updated)
- ✅ `e2e-tests/` and test files
- ✅ `LICENSE`, `README.md`
- ✅ `tools/` (Go tool dependencies)

## Files to Drop
- ❌ `package.json` - No longer using pnpm
- ❌ `env/index.js` - Replaced with simpler env vars
- ❌ `node_modules/` - Not needed
- ❌ `dist/` - Will be regenerated
- ❌ `.vscode/`, `.idea/` - User-specific

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Breaking import paths for downstream users | High | Document migration in changelog, provide transition period |
| Image URL changes | Medium | Use same quay.io/kubesmarts URLs, maintain compatibility |
| Lost git history | Low | Accept clean break OR use git subtree (adds complexity) |
| Build environment complexity | Medium | Test thoroughly, document new build process |

## Success Criteria

- [ ] `make build` produces working binary
- [ ] All unit tests pass
- [ ] E2E tests pass with local operator
- [ ] CI builds multi-platform binaries
- [ ] Documentation updated
- [ ] No dependencies on incubator-kie-tools repo

## Post-Migration

1. Update incubator-kie-tools to mark kn-plugin-workflow as deprecated
2. Add note pointing to logic-operator repo
3. Release first version from logic-operator (e.g., v2.0.0 to indicate new home)

## Notes

- **Version Strategy:** Recommend decoupling CLI version from operator version (semantic versioning independently)
- **Backwards Compatibility:** Consider keeping `kn-workflow` binary name but also providing `logic-cli` alias
- **Import Path:** `github.com/kubesmarts/logic-operator/cli` is clean and clear
