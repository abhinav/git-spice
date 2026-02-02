---
icon: material/tooltip-check
title: Recipes
description: >-
  Common tasks, workflows, and configuration for git-spice.
---

# Recipes

## Customization

### Auto-track branches on checkout

<!-- gs:version v0.23.0 -->

Git's post-checkout hook can be used
to invoke git-spice automatically when a branch is checked out,
and track the branch with git-spice if it is not already tracked.

**Prerequisites:**

- git-spice <!-- gs:version v0.23.0 -->
- Git hooks are enabled
  (this is the default, but certain setups may disable them)
- The repository is already initialized with git-spice ($$gs repo init$$)

**Steps:**

1. Copy this script under `.git/hooks/post-checkout` in your repository.

    ??? example ".git/hooks/post-checkout"

        ```bash
        #!/usr/bin/env bash
        set -euo pipefail

        # post-checkout is invoked with:
        #   $1 - ref of the previous HEAD
        #   $2 - ref of the new HEAD
        #   $3 - 1 for a branch checkout, 0 for a file checkout
        shift # old SHA
        shift # new SHA
        checkout_type=$1

        # Ignore non-branch checkouts.
        if [[ "$checkout_type" -eq 0 ]]; then
            exit 0
        fi

        # Don't do anything if this was invoked during a git-spice operation;
        # if git-spice runs checkout, let it do what it's doing without interference.
        if [[ -n "${GIT_SPICE:-}" ]]; then
            exit 0
        fi

        # post-checkout hook does not receive the branch name,
        # so get it from the new HEAD.
        branch_name=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
        if [[ -z "$branch_name" ]]; then
            exit 0 # not a branch
        fi

        # ...and verify it's actually a local branch.
        if ! git show-ref --verify --quiet refs/heads/"$branch_name"; then
            exit 0
        fi

        # Don't attempt to track trunk.
        trunk_name=$(gs trunk -n 2>/dev/null || echo "master")
        if [[ "$branch_name" == "$trunk_name" ]]; then
            exit 0
        fi

        # Check if the branch is already tracked by git-spice
        # by poking at the internals. (gs ls --json will be slower here.)
        #
        # Warning: This may break if git-spice's internal storage format changes.
        if ! git rev-parse --verify --quiet refs/spice/data:branches/"$branch_name" >/dev/null; then
            echo >&2 "Branch not tracked with git-spice: '$branch_name'. Tracking it now..."

            # We use 'downstack track' so that if there are any untracked branches
            # downstack from this one, they get tracked too.
            gs downstack track "$branch_name"
        fi
        ```

2. Make sure the script is executable.

    ```bash
    chmod +x .git/hooks/post-checkout
    ```

3. Test it out by checking out an untracked branch.

    ```bash
    git checkout -b my-feature main
    ```

**How this works:**

- The post-checkout hook is invoked by Git after a checkout operation,
  whether with `git checkout` or `git switch`.
- The script checks `GIT_SPICE` (added in <!-- gs:version v0.23.0 -->)
  to ensure it does not interfere
  with git-spice's own operations (e.g. $$gs branch checkout$$).
- It checks whether the new branch is already tracked by git-spice
  by looking inside its internal storage ([Internals](../guide/internals.md)).
- If the branch is not tracked, it invokes $$gs downstack track$$
  to track the branch and any untracked branches downstack from it.

## Workflows

### Create branches without committing

<!-- gs:version v0.5.0 -->

The default workflow for git-spice forces you to commit immediately
to new branches: $$gs branch create$$ will create a new branch,
and commit staged changes to it immediately,
or if there are no staged changes, it will create an empty commit.

If you have a workflow where you prefer to create branches first,
and then work on them, you can use the following to adjust the workflow:

- Configure $$gs branch create$$ to never commit by default
  by setting $$spice.branchCreate.commit$$ to false.

    ```bash
    git config --global spice.branchCreate.commit false
    ```

- Use $$gs branch create$$ as usual to create branches.
  Changes will not be committed automatically anymore.

    ```bash
    gs branch create my-branch
    ```

- If, for some branches, you do want to commit staged changes upon creation,
  use the `--commit` flag or `-m`/`--message` (which always implies `--commit`).

    ```bash
    gs branch create my-branch --commit
    gs branch create my-branch -m "Commit message"
    ```

### Working with unsupported remotes

<!-- gs:version v0.6.0 -->

If you're using a Git hosting service that is not supported by git-spice
(e.g. Bitbucket, SourceHut, etc.),
you can use git-spice to manage your branches locally without any issues.
However, when it comes to pushing branches to the remote,
there are some options that can help your workflow.

- Stop git-spice from trying to submit changes to the service
  by setting $$spice.submit.publish$$ to false.

    ```bash
    git config spice.submit.publish false
    ```

- $$gs repo sync$$ will detect branches that were merged
  with merge commits or fast-forwards, and delete them locally.
  For branches that were merged by rebasing or squashing,
  you'll need to manually delete merged branches with $$gs branch delete$$.

## Tasks

### Import a Pull Request from GitHub

git-spice can recognize and manage GitHub Pull Requests
that were not created using git-spice.

**Steps:**

1. Check the PR out locally.
   For example, if you're using the GitHub CLI:

    ```bash
    gh pr checkout 123
    ```

2. Track the branch with git-spice.

    ```bash
    gs branch track
    ```

3. Attempt to re-submit the branch.
   git-spice will automatically detect the existing open PR for it,
   and associate the branch with that PR.

    ```bash
    gs branch submit
    ```

### Track an existing stack

If you have an existing, manually managed stack of branches,
you can import it into git-spice in one of two ways:

=== "With $$gs downstack track$$ (recommended)"

    <!-- gs:version v0.19.0 -->

    The fastest way to track an existing stack
    is to use $$gs downstack track$$ from the topmost branch.

    **Steps:**

    1. Check out the topmost branch.

        ```bash
        git checkout feat3
        ```

    2. Verify that the branches in the stack are reachable from each other
      in the correct order.
      You can use `git log --graph` or a visual tool for this.

        ```bash
        git log --oneline --graph
        ```

    3. Track the entire stack at once.

        ```bash
        gs downstack track
        ```

        This will look for branches down the commit history
        and prompt you to track branches as it encounters them.

    4. You may now use git-spice to manage the stack as usual.

=== "With $$gs branch track$$ (manual)"

    You can also track each branch individually
    with repeated use of $$gs branch track$$.

    **Steps:**

    1. Check out the base branch and initialize git-spice.

        ```bash
        git checkout main
        gs repo init
        ```

    2. Check out the first branch in the stack and track it.

        ```bash
        git checkout feat1
        gs branch track
        ```

    3. Repeat the previous step for each branch in the stack.

        ```bash
        git checkout feat2
        gs branch track
        # ... repeat until done ...
        ```

    4. You may now use git-spice to manage the stack as usual.
