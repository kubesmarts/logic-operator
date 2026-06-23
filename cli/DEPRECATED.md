# CLI Module - DEPRECATED

## Status

**This module is currently deprecated and excluded from the Logic Operator v2.0 refactoring.**

## Background

The Logic Operator is undergoing a major architectural overhaul from SonataFlow to Quarkus Flow (v2.0). 
During this transition, the CLI module has been temporarily excluded from the codebase to:

1. Avoid maintenance burden during the core operator refactoring
2. Allow time to properly assess CLI requirements in the v2.0 architecture
3. Prevent breaking changes to the CLI while the operator API evolves

## Current State

- **Build Status**: Excluded from `go.work` - will not compile with main operator
- **Maintenance**: No active development or bug fixes
- **Testing**: Excluded from CI/CD pipelines

## Future

The CLI's role and implementation will be reassessed after the v2.0 core operator work is complete. 
Possible outcomes:

- **Redesign**: CLI rebuilt to work with new v2.0 CRDs (LogicPlatform, LogicFlowDefinition, etc.)
- **Archive**: Deprecated permanently if CLI use cases are better served by kubectl/kustomize
- **Extract**: Moved to separate repository with independent lifecycle

## Migration

For users currently relying on the CLI:

- Use `kubectl` and `kustomize` to manage Logic Operator v2.0 resources directly
- Existing CLI workflows will not be compatible with v2.0 operator

## Related

- [ADR-001: Logic Operator v2.0 Architecture](../adr/001-logic-operator-v2-architecture.md)
- [EPIC 0: Codebase Cleanup](https://github.com/kubesmarts/logic-operator/issues/3)
