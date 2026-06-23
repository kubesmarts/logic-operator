# CLI Migration: Current Metadata Values

**Source:** `apache/incubator-kie-tools/packages/kn-plugin-workflow`  
**Date:** 2026-06-22  
**Purpose:** Document current values for migration to logic-operator

## Build Metadata (Injected via ldflags)

### Version Information
```go
// pkg/metadata/version.go
var PluginVersion string  // Injected from package.json (currently "0.0.0")
```

### Image References
```go
// From env/index.js composition
var DevModeImage string   // From @kie-tools/sonataflow-devmode-image/env
var BuilderImage string   // From @kie-tools/sonataflow-builder-image/env
```

**Current image paths (from kie-tools):**
- DevMode: `quay.io/apache/incubator-kie-sonataflow-devmode:main`
- Builder: `quay.io/apache/incubator-kie-sonataflow-builder:main`

### Quarkus Configuration
```go
// From env/index.js
var QuarkusPlatformGroupId string    // Default: io.quarkus.platform
var QuarkusPlatformArtifactId string // Default: quarkus-bom
var QuarkusVersion string            // From root-env
```

## Migration Strategy

### New Metadata Structure
```go
// cli/pkg/metadata/metadata.go (NEW)
package metadata

// Injected at build time via -ldflags
var (
    PluginVersion   string // Git tag (e.g., "v2.0.0")
    QuarkusVersion  string // From root .env
    BuilderImage    string // From root .env
    DevModeImage    string // From root .env
)
```

### Build Command (NEW)
```makefile
# cli/Makefile
include ../.env

VERSION ?= $(shell git describe --tags --always)

LDFLAGS := -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.PluginVersion=$(VERSION) \
           -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.QuarkusVersion=$(QUARKUS_VERSION) \
           -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.BuilderImage=$(BUILDER_IMAGE) \
           -X github.com/kubesmarts/logic-operator/cli/pkg/metadata.DevModeImage=$(DEVMODE_IMAGE)

build:
	go build -ldflags="$(LDFLAGS)" -o bin/kn-workflow cmd/main.go
```

### Root .env Values (inherit from parent)
```bash
# From /Users/ricferna/dev/github/kubesmarts/logic-operator/.env

# Quarkus
QUARKUS_VERSION=3.8.1

# Images (updated to kubesmarts registry)
BUILDER_IMAGE=quay.io/kubesmarts/logic-operator-builder:main
DEVMODE_IMAGE=quay.io/kubesmarts/logic-operator-devmode:main

# Or keep apache images initially:
# BUILDER_IMAGE=quay.io/apache/incubator-kie-sonataflow-builder:main
# DEVMODE_IMAGE=quay.io/apache/incubator-kie-sonataflow-devmode:main
```

## Files to Remove
- ❌ `env/index.js` - Replaced by Makefile + .env
- ❌ `package.json` - No longer using pnpm
- ❌ All Node.js build wrapper code

## Files to Create/Update
- ✅ `cli/pkg/metadata/metadata.go` - Add QuarkusVersion, BuilderImage, DevModeImage vars
- ✅ `cli/Makefile` - Simplified build with ldflags injection
- ✅ `cli/README.md` - Update build instructions
