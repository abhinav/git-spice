---
title: PR stacks
icon: octicons/git-pull-request-16
description: >-
  Create and update stacked pull requests from a stack of branches.
---

# Pull Request Stacks

## Submitting pull requests

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
pull requests will be created for branches that don't already have them,
and updated for branches that do.

For new pull requests, these commands will prompt you for PR information.
For example:

```freeze language="ansi"
--8<-- "captures/branch-submit.txt"
```

!!! important

    Be aware that for stacks with multiple branches,
    you must have write access to the repository
    so that you can push branches to it.
    See [Limitations](limits.md) for more information.

Pull Requests created by git-spice will include a comment at the top
with a visual representation of the stack,
and the position of the current branch in it.

![Example of a stack comment](../img/stack-comment.png)

### Non-interactive submission

Use the `--fill` flag provided by all the above commands
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

    Pull requests may be marked as draft or ready for review
    non-interactively with the `--draft` and `--no-draft` flags.

    By default, the submit commands will leave
    the draft state of existing PRs unchanged.
    If the `--draft` or `--no-draft` flags are provided,
    the draft state of all PRs will be set accordingly.

### Force pushing

<!-- gs:version v1.0.0 -->

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
