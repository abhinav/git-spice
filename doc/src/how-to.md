---
icon: material/tooltip-check
title: How-to
description: >-
  Common tasks and how to perform them with git-spice.
---

# How-to...

## Manage an existing stack with git-spice

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

## Import an existing Pull Request

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
