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
