---
title: Your first stacked CRs
icon: material/slide
description: >-
  Turn a stack of branches into Change Requests.
  Update them all in one go.
prev_page: stack.md
next_page: ../guide/index.md
---

# Your first stacked Pull Requests

This page will walk you through creating and updating
*GitHub Pull Requests* or *GitLab Merge Requests* with git-spice.
git-spice refers to these as *Change Requests* (CRs).

## Prerequisites

- [x] [Create a stack of branches](stack.md).
      For this tutorial, we'll assume the following stack.
      Go back to the previous page if you need to create it.

      ```pikchr center="false"
      linerad = 0.05in
      text "main"
      up; line go up 0.15in then go right 0.15in
      text "feat1"
      up; line go up 0.15in then go right 0.15in
      text "feat2"
      ```

- [x] Set up a GitHub or GitLab repository.

    ??? info "Optional: Create an experimental repository"

        If you're following along with the tutorial,
        you may want to create a new repository on GitHub
        to experiment with instead of using a real project.

        To do this, if you have the GitHub or GitLab CLI installed,
        run the following inside your experimental repository:

        === "<!-- gs:github -->"

            ```bash
            gh repo create gs-playground \
              --public \
              --source=$(pwd) \
              --push
            ```

            If you don't have the GitHub CLI installed,
            go to <https://github.com/new> and follow the instructions there.

        === "<!-- gs:gitlab -->"

            ```bash
            glab repo create gs-playground --public
            ```

            If you don't have the GitLab CLI installed,
            go to <https://gitlab.com/projects/new> and follow the instructions there.

## Create a Change Request

1. Check out `feat1`.

    === "git"

        ```bash
        git checkout feat1
        ```

    === "gs"

        ```bash
        gs branch checkout feat1
        ```

    === "gs shorthand"

        ```bash
        gs bco feat1
        ```

2. Submit the change.

    === "gs"

        ```bash
        gs branch submit
        ```

    === "gs shorthand"

        ```bash
        gs bs
        ```

3. Follow the prompts on-screen to create the CR.
   For example:

   ```freeze language="ansi"
   --8<-- "captures/branch-submit.txt"
   ```

## Stack on top

1. Check out `feat2`.

    ```bash
    gs up
    ```

2. Submit a CR.

    ```bash
    gs branch submit
    ```

3. Follow the prompts on-screen to create the CR.

The new CR will now be stacked on top of the previous one:
the remote will show `feat1` as the base branch for the CR.

## Modify mid-stack

Modify the `feat1` branch and update the CR.

1. Check out `feat1`.

    ```bash
    gs down
    ```

2. Make some changes.

    === "gs"

        ```bash
        echo "Not bad, how about you?" >> hello.txt
        git add hello.txt
        gs commit create -m "follow up"
        ```

    === "gs shorthand"

        ```bash
        echo "Not bad, how about you?" >> hello.txt
        git add hello.txt
        gs cc -m "follow up"
        ```

    !!! info

        We use the $$gs commit create$$ command here.
        This will commit to the current branch,
        and rebase the upstack branches on top of the new commit.

3. Update all CRs in the stack.

    ```bash
    gs stack submit
    ```

This will push to both CRs in the stack.
If one of the branches was not submitted yet,
it will prompt you to create a CR for it.

## Merge a CR

1. Open up the CR for `feat1` in your browser
   and merge it into `main`.

2. Run the following command to sync the stack with the trunk:

    ```bash
    gs repo sync
    ```

    This will delete `feat1` locally,
    and rebase `feat2` on top of `main`.

3. Submit the CR for `feat2` to update the pull request
   if necessary.

    ```bash
    gs branch submit
    ```

## Summary

**This section covered:**

- [x] $$gs branch submit$$ creates or updates a CR for the current branch.
- [x] $$gs stack submit$$ creates or updates CRs for the entire stack.
- [x] $$gs repo sync$$ syncs the stack with the trunk branch,
      deletes merged branches, and rebases the remaining branches.

## Next steps

- [ ] Explore other submit commands:
      $$gs upstack submit$$, $$gs downstack submit$$
- [ ] Browse the [User Guide](../guide/index.md) to learn more
