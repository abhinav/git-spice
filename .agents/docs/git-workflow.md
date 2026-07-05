# Git Workflow

Use this guide when changing Git state:
branches, commits, stack operations, rebases, pushes,
or pull request publishing.

## Preserve User State

Do not treat incidental repository state as part of the task.
Dirty files, staged hunks, untracked files,
and mixed staged or unstaged paths may be intentional.

Inspect only the state needed for the requested operation.
Stage only the intended files.
Report unrelated state instead of rearranging it.

## Branches And Commits

Use git-spice commands for stack, branch,
and commit operations in this repository.
Do not create commits with raw `git commit`.

Before committing, confirm the current branch.
Do not commit new work directly on `main`
unless the user explicitly asks for that.

After branch or commit operations, verify state with the relevant commands:

```bash
git status --short --branch
git branch --show-current
git log -1 --oneline --decorate
git-spice ls --no-prompt
```

## Pull Requests

Local commits and pull request updates are separate operations.
Do not push, publish, submit, or update a pull request
unless the user asks for that action.

If the user asks only for a local commit, leave the PR untouched.

## Recovery

If a git-spice operation is interrupted,
inspect the current branch, rebase state, and stack state before continuing.
Use the non-interactive git-spice recovery path
that matches the operation in progress.

Do not use destructive Git commands
unless the user explicitly requested the destructive operation
or approved the recovery step.
