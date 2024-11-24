---
title: Internals
icon: octicons/codescan-16
description: >-
  A look at the inner workings of git-spice for the curious.
---

# git-spice internals

!!! warning

    This page covers internal details about the implementation of git-spice.
    Most users do not need to read this.
    It is presented here for the curious,
    and in the interest of transparency.

    Do not rely on internal details to remain stable.
    These may change at any time.

## Local storage

git-spice stores information about your repository and branches
in a local Git ref named: `refs/spice/data`.

Information is stored in JSON files with roughly the following structure:

```tree
repo            # repository-level information
templates       # cached change templates
rebase-continue # information about ongoing operations
branches        # branch tracking information
    feat1
    feat2
    ...
prepared        # ephemeral per-branch PR form information
    feat1
    feat2
    ...
```

git-spice operations that manipulate this information
will usually include what prompted the change.

You can explore this information by running
the following command in a repository using git-spice:

```bash
git log --patch refs/spice/data
```

## Git interactions

git-spice does not use a third-party Git implementation.
All operations are performed directly against the Git CLI,
often relying on Git's [plumbing commands](https://git-scm.com/book/en/v2/Git-Internals-Plumbing-and-Porcelain).

!!! question "Why?"

    Most third-party Git implementations trail behind in feature parity.
    For example, many third-party implementations can misbehave when you make
    use of advanced features like
    [git worktree](https://git-scm.com/docs/git-worktree),
    [git sparse-checkout](https://git-scm.com/docs/git-sparse-checkout),
    or [sparse indexes](https://git-scm.com/docs/sparse-index).

    By relying on the highly scriptable and machine-consumable Git plumbing
    we don't have to deal with those issues.
