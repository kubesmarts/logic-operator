# Operator SDK Migration Analysis - Logic Operator v2.0

## Current State

**Operator SDK Version**: v1.35.0 (Current: June 2024)  
**Latest Available**: v1.42.2 (Latest: June 2025)  
**Kubebuilder Layout**: v4  
**Gap**: 7 minor versions behind

## Current Structure

```
config/          ~2.1MB  (CRDs, RBAC, manager, kustomize configs)
bundle/          ~1.9MB  (OLM bundle manifests)
Makefile         562 lines
PROJECT          46 lines (references 4 deleted CRDs)
```

### What's in config/

- **config/crd/**: CRD definitions for deleted SonataFlow v1alpha08 resources
- **config/rbac/**: RBAC for deleted controllers
- **config/manager/**: Manager deployment referencing old image
- **config/samples/**: Sample CRs for deleted CRDs
- **config/default/**: Kustomization for deleted resources
- **config/prometheus/**: ServiceMonitor for old metrics
- **config/manifests/**: OLM metadata for old CRDs
- **config/scorecard/**: Scorecard config for old CRDs

### What's in bundle/

- OLM bundle for SonataFlow v1alpha08 CRDs
- CSV (ClusterServiceVersion) with old metadata
- All deleted CRD schemas

## Recommendation: **START FRESH** ✅

### Why Start Fresh?

1. **100% of config/ is invalid**
   - All CRDs reference deleted v1alpha08 API
   - All RBAC rules reference deleted controllers
   - All samples reference deleted resources
   - Manager config references old images/args

2. **Clean v2.0 design**
   - New CRD group: `logic.kubesmarts.io` (vs old `sonataflow.org`)
   - New CRD names: LogicPlatform, LogicFlowService, LogicFlowDefinition, LogicFlowRuntime
   - No backward compatibility needed
   - Fresh RBAC based on actual v2.0 needs

3. **Upgrade to latest SDK v1.42.2**
   - 7 minor versions of improvements
   - Better kubebuilder v4 support
   - Latest OLM bundle format
   - Modern deployment patterns

4. **Minimal effort to recreate**
   - `operator-sdk init` takes 30 seconds
   - `operator-sdk create api` generates scaffolding
   - Can copy preserved code (discovery, utils) after scaffold
   - Fresh Makefile with modern targets

5. **Avoid technical debt**
   - No leftover kustomize patches for deleted resources
   - No outdated RBAC rules
   - No confusing PROJECT file referencing old APIs
   - Clean git history for v2.0

## Migration Plan

### Phase 1: Scaffold Fresh Project (1-2 hours)

```bash
# 1. Backup current state
mkdir -p /tmp/logic-operator-backup
cp -r . /tmp/logic-operator-backup/

# 2. Remove old scaffolding
rm -rf config/ bundle/ PROJECT Makefile

# 3. Initialize fresh project with latest SDK
operator-sdk init \
  --domain kubesmarts.io \
  --repo github.com/kubesmarts/logic-operator \
  --project-name logic-operator

# 4. Create v1 API group
operator-sdk create api \
  --group logic \
  --version v1 \
  --kind LogicPlatform \
  --resource \
  --controller

operator-sdk create api \
  --group logic \
  --version v1 \
  --kind LogicFlowService \
  --resource \
  --controller

operator-sdk create api \
  --group logic \
  --version v1 \
  --kind LogicFlowDefinition \
  --resource \
  --controller

operator-sdk create api \
  --group logic \
  --version v1 \
  --kind LogicFlowRuntime \
  --resource \
  --controller
```

### Phase 2: Restore Preserved Code (1 hour)

```bash
# Copy preserved utilities
cp -r /tmp/logic-operator-backup/internal/controller/discovery internal/controller/
cp -r /tmp/logic-operator-backup/internal/controller/cfg internal/controller/
cp -r /tmp/logic-operator-backup/internal/controller/knative internal/controller/
cp -r /tmp/logic-operator-backup/internal/controller/openshift internal/controller/
cp -r /tmp/logic-operator-backup/utils utils/

# Copy other preserved files
cp /tmp/logic-operator-backup/log/*.go log/
cp /tmp/logic-operator-backup/CLAUDE.md .
cp /tmp/logic-operator-backup/cli/DEPRECATED.md cli/
```

### Phase 3: Implement CRD Types (2-3 hours)

Based on ADR-001, implement:
- `api/v1/logicplatform_types.go`
- `api/v1/logicflowservice_types.go`
- `api/v1/logicflowdefinition_types.go`
- `api/v1/logicflowruntime_types.go`

### Phase 4: Generate Manifests (30 min)

```bash
make generate
make manifests
make bundle
```

Fresh, clean config/ and bundle/ generated from v2.0 CRDs.

### Phase 5: Update Workflows (30 min)

Update `.github/workflows/` to use SDK v1.42.2:
```yaml
OPERATOR_SDK_VERSION=v1.42.2
```

## What We Lose (Nothing Important)

- ❌ Old SonataFlow CRD configs (already deleted from API)
- ❌ Old RBAC rules (will regenerate for v2.0)
- ❌ Old samples (will create new v2.0 samples)
- ❌ Old bundle (will generate fresh for v2.0)

## What We Keep

- ✅ All Go code (cmd/, internal/, utils/, api/condition_types.go, api/status_types.go)
- ✅ Discovery subsystem
- ✅ Generic utilities
- ✅ git history
- ✅ README, docs, workflows (updated)
- ✅ .env configuration

## Alternative: Incremental Cleanup (NOT RECOMMENDED)

If you want to keep existing structure:
1. Manually delete all v1alpha08 references in config/
2. Manually update PROJECT file
3. Manually update Makefile targets
4. Manually update RBAC rules
5. Manually update bundle/

**Estimated effort**: 4-6 hours  
**Result**: Still have outdated SDK v1.35.0, leftover cruft

vs.

**Fresh start effort**: 3-5 hours  
**Result**: Latest SDK v1.42.2, clean structure, no technical debt

## Recommendation

**DELETE** the following and start fresh:
```
config/
bundle/
PROJECT
Makefile
```

**KEEP** everything else:
```
api/                 (status_types.go, condition_types.go, version/)
cmd/
internal/controller/ (discovery/, cfg/, knative/, openshift/)
utils/
log/
.github/
README.md
CLAUDE.md
cli/DEPRECATED.md
.env*
go.mod, go.work
```

## Next Steps

1. Get user approval for fresh scaffold
2. Run operator-sdk init with new domain/group
3. Create v1 APIs for 4 CRDs
4. Implement CRD types based on ADR-001
5. Generate fresh manifests
6. Test build and deployment

## Benefits Summary

✅ Latest Operator SDK (v1.42.2)  
✅ Clean v2.0 CRD structure  
✅ Modern kubebuilder v4 scaffolding  
✅ No technical debt from v1alpha08  
✅ Fresh RBAC matching actual needs  
✅ Clean OLM bundle for v2.0  
✅ Easier to maintain going forward  
