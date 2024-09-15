---
icon: material/tooltip-check
title: Recipes
description: >-
  Common tasks, workflows, and configuration for git-spice.
---

# Recipes

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
  use the `--commit` flag.

    ```bash
    gs branch create my-branch --commit
    ```

### Working with non-GitHub remotes

<!-- gs:version unreleased -->

If you're using a Git hosting service that is not GitHub
(e.g. GitLab, Bitbucket, etc.),
you can use git-spice to manage your branches locally without any issues.
However, when it comes to pushing branches to the remote,
there are some options that can help your workflow.

- Stop git-spice from trying to create GitHub Pull Requests
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
you can import it into git-spice with repeated use of $$gs branch track$$.

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
