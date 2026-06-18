# Migration from SonataFlow Operator

This repository is a continuation of the Apache KIE SonataFlow Operator, migrated from the
[Apache KIE Tools monorepo](https://github.com/apache/incubator-kie-tools) to a standalone
repository.

## What Changed

### Repository Location
- **Old:** `github.com/apache/incubator-kie-tools/packages/sonataflow-operator`
- **New:** `github.com/kubesmarts/logic-operator`

### Go Module Paths
All import paths have been updated:
```go
// Old
import "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"

// New
import "github.com/kubesmarts/logic-operator/api/v1alpha08"
```

### Build System
- **Removed:** pnpm/npm dependencies and build-env configuration
- **Added:** `.env` file-based configuration
- **Simplified:** Pure Go/Make build system

### Configuration
Copy `.env.example` to `.env` and customize:
```bash
cp .env.example .env
```

Set environment variables to override defaults:
```bash
export VERSION=2.0.1
export REGISTRY=quay.io
make build
```

## Git History

The full commit history from the monorepo has been preserved. All commits that touched
the `packages/sonataflow-operator` directory are included with paths rewritten to the
repository root.

Original migration commit: `v2.0.0-migration-base`

## License

This project maintains the Apache License 2.0 from the original project. All existing
files retain their original Apache Software Foundation copyright headers. New files
contributed after the migration use Kubesmarts copyright headers.

## For Existing Users

If you were using the operator from the monorepo:

1. Update your Go module imports to `github.com/kubesmarts/logic-operator`
2. Run `go mod tidy` to update dependencies
3. No changes to CRDs or runtime behavior - full compatibility maintained

## Migration Date

Migration completed: June 18, 2026
