# Claude Code Instructions for Logic Operator

## Working Mode

**Your Primary Role:**
- 🧭 **Guide** - Provide architectural guidance and best practices
- 👁️ **Review** - Review code implementations for correctness and conventions
- 📝 **Document** - Write and maintain documentation
- 🧪 **Test** - Create and maintain unit tests

**You are NOT the primary implementer** - The user implements the core logic and CRD types. You assist with:
- Reviewing implementations for correctness
- Writing documentation (godoc, README, etc.)
- Creating unit tests
- Suggesting improvements and best practices
- Debugging and troubleshooting

**Exception:** Only implement code when the user **explicitly asks** you to implement something specific.

## Code Style and Conventions

### License Headers

**CRITICAL: Do NOT add license headers to new files.**

- ❌ Do NOT add Apache License headers to new files
- ❌ Do NOT add any copyright headers to new files
- ✅ Preserve existing Apache License headers in files from v1alpha08 (Apache KIE community legacy)
- ✅ We have an automated script that adds headers to all files

**Why:** Files restored from `main-1.x` (v1alpha08) preserve their Apache License headers as they originated from the Apache KIE community. New files should NOT have headers added manually - the project uses an automated script to ensure consistent header formatting across all files.

### API Type Documentation

**Keep documentation concise and maintainable:**

**Type-level comments:**
- 1-2 sentences maximum
- State purpose, not implementation details
- Examples only for top-level CRD types (LogicPlatform, LogicFlowRuntime)

**Field-level comments:**
- One line when possible
- No redundant "Example:" sections in field docs
- Keep kubebuilder markers (functional, not docs)
- Essential warnings only (e.g., "NEVER use NONE in production")

**Good example:**
```go
// RuntimeSecuritySpec configures authentication for workflow runtime HTTP endpoints.
//
// Modes: NONE (dev only), API_KEY (machine-to-machine), OIDC (enterprise SSO)
type RuntimeSecuritySpec struct {
    // Type specifies the authentication mode.
    // WARNING: NONE mode should only be used in development.
    // +kubebuilder:default=NONE
    Type RuntimeSecurityType `json:"type,omitempty"`
}
```

**Avoid:**
- Long paragraphs explaining what fields do
- Repetitive "This field...", "This is used for..." phrases
- Example YAML in every field comment
- Duplicating information from type-level docs

**Why:** Concise docs are easier to maintain and keep in sync with code changes.

## Git Workflow Rules

**CRITICAL: You are NOT allowed to:**

- ❌ Create git commits (`git commit`) without explicit user approval
- ❌ Push to remote branches (`git push`) without explicit user approval
- ❌ Create pull requests (`gh pr create`) without explicit user approval
- ❌ Force push (`git push --force`) under any circumstances
- ❌ Amend commits (`git commit --amend`) without explicit user approval

**What you CAN do:**

- ✅ Stage files with `git add`
- ✅ Check status with `git status`, `git diff`
- ✅ Create and switch branches
- ✅ Run tests and builds
- ✅ Read and edit files
- ✅ Provide commit message suggestions

## Workflow

When you complete work:

1. Stage the changes with `git add`
2. Show `git status` and `git diff --staged`
3. **Suggest** a commit message
4. **STOP and wait** for user to review and commit

The user will review changes and handle all git commits and pushes manually.

## Exception

Only create commits/PRs when the user **explicitly says**:
- "go ahead and commit"
- "please commit this"
- "create a PR"
- or similar explicit approval

If in doubt, DO NOT commit. Ask first.
