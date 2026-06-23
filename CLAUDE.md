# Claude Code Instructions for Logic Operator

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
