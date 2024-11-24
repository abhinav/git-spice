---
title: Submitting stacks
icon: octicons/git-pull-request-16
description: >-
  Create and update stacked change requests from a stack of branches.
---

# Working with remote repositories

!!! note

    This page assumes you are using one of the supported Git forges.
    These are:

    - :simple-github: **GitHub**
    - :simple-gitlab: **GitLab** (<!-- gs:version unreleased -->)

    If you're using a different service,
    you can still use git-spice,
    but some features may not be available.

    See:

    - [:material-tooltip-check: Recipes > Working with unsupported remotes](../recipes.md#working-with-unsupported-remotes)
    - [:material-frequently-asked-questions: FAQ > Will git-spice add support for other Git hosting services](../faq.md#will-git-spice-add-support-for-other-git-hosting-services)

## Submitting change requests

!!! info

    git-spice uses the term *Change Request* to refer to submitted branches.
    These corespond to Pull Requests on GitHub,
    and to Merge Requests on GitLab.

When your local changes are ready,
use the following commands to submit your changes upstream:

- $$gs branch submit$$ (or $$gs branch submit|gs bs$$)
  submits the current branch
- $$gs downstack submit$$ (or $$gs downstack submit|gs dss$$)
  submits the current branch and all branches below it
- $$gs upstack submit$$ (or $$gs upstack submit|gs uss$$)
  submits the current branch and all branches above it
- $$gs stack submit$$ (or $$gs stack submit|gs ss$$)
  submits all branches in the stack

Branch submission is an idempotent operation:
change requests will be created for branches that don't already have them,
and updated for branches that do.

For new change requests, these commands will prompt you for CR information.
For example:

```freeze language="ansi"
--8<-- "captures/branch-submit.txt"
```

!!! important

    Be aware that for stacks with multiple branches,
    you must have write access to the repository
    so that you can push branches to it.
    See [Limitations](limits.md) for more information.

### Navigation comments

Change Requests created by git-spice will include a navigation comment
at the top with a visual representation of the stack,
and the position of the current branch in it.

=== "GitHub"

    ![Example of a stack navigation comment on GitHub](../img/stack-comment.png)

=== "GitLab"

    ![Example of a stack navigation comment on GitLab](../img/stack-comment-glab.png)

This behavior may be changed with the $$spice.submit.navigationComment$$
configuration key.

### Non-interactive submission

Use the `--fill` flag (or `-c` since <!-- gs:version v0.3.0 -->)
provided by all the above commands
to fill in the PR information from commit messages
and skip the interactive prompts.

```freeze language="terminal"
{green}${reset} gs stack submit --fill
{green}INF{reset} Created #123: https://github.com/abhinav/git-spice/pull/123
{green}INF{reset} Created #125: https://github.com/abhinav/git-spice/pull/125
```

Additionally, with $$gs branch submit$$,
you may also specify title and body directly.

```freeze language="terminal"
{green}${reset} gs branch submit {gray}\{reset}
    --title {blue}"Fix a bug"{reset} {gray}\{reset}
    --body {blue}"This fixes a very bad bug."{reset}
{green}INF{reset} Created #123: https://github.com/abhinav/git-spice/pull/123
```

!!! info "Setting draft status non-interactively"

    Change requests may be marked as draft or ready for review
    non-interactively with the `--draft` and `--no-draft` flags.

    By default, the submit commands will leave
    the draft state of existing PRs unchanged.
    If the `--draft` or `--no-draft` flags are provided,
    the draft state of all PRs will be set accordingly.

### Force pushing

<!-- gs:version v0.2.0 -->

```freeze language="terminal" float="right"
{green}${reset} gs branch submit --force
```

By default, git-spice will refuse to push to branches
if the operation could result in data loss.
To override these safety checks
and push to a branch anyway, use the `--force` flag.

## Syncing with upstream

To sync with the upstream repository,
use $$gs repo sync$$ (or $$gs repo sync|gs rs$$).

```freeze language="terminal" float="right"
{green}${reset} gs repo sync
{green}INF{reset} main: pulled 3 new commit(s)
{green}INF{reset} feat1: #123 was merged
{green}INF{reset} feat1: deleted (was 9f1c9af)
```

This will update the trunk branch (e.g. `main`)
with the latest changes from the upstream repository,
and delete any local branches whose PRs have been merged.

## Importing open CRs

You can import an existing open CR into git-spice
by checking it out locally, tracking the branch with git-spice,
and re-submitting it.

For example:

=== "GitHub"

    ```freeze language="terminal"
    {gray}# Check out the PR locally{reset}
    {green}${reset} gh pr checkout 359

    {gray}# Track it with git-spice{reset}
    {green}${reset} gs branch track

    {gray}# Re-submit it{reset}
    {green}${reset} gs branch submit
    {green}INF{reset} comment-recovery: Found existing CR #359
    {green}INF{reset} CR #359 is up-to-date: https://github.com/abhinav/git-spice/pull/359
    ```

=== "GitLab"

    ```freeze language="terminal"
    {gray}# Check out the MR locally{reset}
    {green}${reset} glab mr checkout 8

    {gray}# Track it with git-spice{reset}
    {green}${reset} gs branch track

    {gray}# Re-submit it{reset}
    {green}${reset} gs branch submit
    {green}INF{reset} reticulating-splines: Found existing CR !8
    {green}INF{reset} CR !8 is up-to-date: https://gitlab.com/abg/test-repo/-/merge_requests/8
    ```

!!! important

    For this to work, the following MUST all be true:

    - The CR is pushed to a branch in the upstream repository
    - The local branch name exactly matches the upstream branch name

This will work even for CRs that were not created by git-spice,
or authored by someone else, as long as the above conditions are met.

In <!-- gs:version v0.5.0 --> or newer,
this will also auto-detect [navigation comments](#navigation-comments)
posted to the PR by git-spice, and update them if necessary.

```freeze language="terminal"
{green}${reset} gs branch submit
{green}INF{reset} comment-recovery: Found existing CR #359
{green}INF{reset} comment-recovery: Found existing navigation comment: {gray}...{reset}
```

