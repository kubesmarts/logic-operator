# CLI E2E Tests - Integration TODO

## Current Status

The CLI has been successfully migrated to the logic-operator repository with all phases complete. However, the CLI e2e test suite currently expects the operator to be installed via OLM (Operator Lifecycle Manager), while our CI workflows deploy the operator directly from source.

## The Issue

**CLI e2e test setup (`cli/e2e-tests/main_test.go`):**
- Calls `InstallOperator()` which uses `operator install` command
- This command creates an OLM Subscription to install the operator from operatorhub.io
- Expects operator to be available as a published OLM package

**Current CI workflow (`cli-e2e.yaml`):**
- Builds operator from source with `make docker-build`
- Loads image into KIND cluster
- Deploys operator directly with `make deploy` (not via OLM)

## Solutions

### Option 1: Create Local OLM Catalog (Recommended for CI)

Build a local OLM catalog with our operator bundle:

1. Build operator bundle image
2. Create catalog image with our bundle
3. Deploy catalog to cluster
4. Modify CLI's operator install to use local catalog
5. Run existing e2e tests unchanged

**Pros:**
- Tests operator installation as users would experience it
- No changes to existing test code
- Tests the full OLM integration

**Cons:**
- More complex CI setup
- Longer build times

### Option 2: Adapt Tests for Direct Deployment

Modify CLI e2e tests to work with directly deployed operator:

1. Add environment variable to skip OLM-based installation
2. Assume operator is already deployed
3. Modify `InstallOperator()` to check for existing deployment instead
4. Update tests to work with pre-deployed operator

**Pros:**
- Simpler CI setup
- Faster test execution
- Works with development workflows

**Cons:**
- Doesn't test OLM installation path
- Requires test code changes

### Option 3: Hybrid Approach

Run two separate test suites:

1. **Unit + Integration tests**: Deploy operator directly, run non-OLM tests
2. **Full E2E with OLM**: Separate workflow with OLM catalog setup

**Pros:**
- Best of both worlds
- Fast feedback from unit/integration tests
- Full OLM coverage in dedicated workflow

**Cons:**
- More complex workflow setup
- Higher maintenance burden

## Recommendation

Start with **Option 2** (adapt tests) for fast iteration, then add **Option 1** (OLM catalog) as a separate nightly/release workflow.

## Test Categories

Current CLI e2e tests:

- `TestCreateProjectSuccess` - ✅ Works without operator
- `TestCreateProjectFail` - ✅ Works without operator
- `TestQuarkusCreateProjectSuccess` - ✅ Works without operator
- `TestQuarkusCreateProjectFail` - ✅ Works without operator
- `TestQuarkusConvertProjectSuccess` - ✅ Works without operator
- `TestQuarkusConvertProjectFailed` - ✅ Works without operator
- `TestQuarkusBuildCommand` - ⚠️ Requires Docker
- `TestRunCommand` - ⚠️ Requires Docker
- `TestQuarkusRunCommand` - ⚠️ Requires Docker
- `TestGenManifestProjectSuccess` - ✅ Works without operator
- `TestDeployProjectSuccess` - ❌ **Requires operator**
- `TestDeployProjectSuccessWithImageDefined` - ❌ **Requires operator**
- `TestDeployProjectSuccessWithoutResultEventRef` - ❌ **Requires operator**

**Strategy:**
1. Short term: Run tests that don't need operator (create, gen-manifest, convert)
2. Medium term: Add Docker-in-Docker for build/run tests
3. Long term: Set up OLM catalog for deploy tests

## Implementation Steps

### Phase 1: Non-Operator Tests (Immediate)

```yaml
- name: Run CLI e2e tests (non-operator)
  working-directory: cli
  run: |
    go test -v ./e2e-tests/... -tags e2e_tests \
      -run "TestCreate|TestQuarkusCreate|TestQuarkusConvert|TestGenManifest" \
      -timeout 10m
```

### Phase 2: Build/Run Tests with Docker (Next)

- Set up Docker-in-Docker in GitHub Actions
- Enable TestQuarkusBuildCommand, TestRunCommand, TestQuarkusRunCommand

### Phase 3: Deploy Tests with Operator (Future)

- Either adapt tests to use pre-deployed operator
- Or set up local OLM catalog with built operator

## Files to Modify

- `.github/workflows/cli-e2e.yaml` - Update test execution
- `cli/e2e-tests/main_test.go` - Add conditional operator installation
- `cli/e2e-tests/operator_helper.go` - Support pre-deployed operator mode

## Success Criteria

- [ ] All non-operator tests pass in CI
- [ ] Build/run tests pass with Docker setup
- [ ] Deploy tests pass with either approach
- [ ] Tests run in under 30 minutes
- [ ] Clear documentation of test categories and requirements
