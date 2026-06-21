---
title: Parallel worktrees
icon: octicons/git-branch-16
description: >-
  Work on a stack from several worktrees at once with anchors, and take the
  whole repository for yourself with exclusive mode.
---

# Parallel worktrees

<!-- gs:version unreleased -->

A single git-spice repository can be worked on from several
[Git worktrees](https://git-scm.com/docs/git-worktree) at once:
one per agent, one for CI, one for you.
The worktrees are the coordination mechanism.
Git already refuses to check out the same branch in two worktrees,
so each worktree owns the slice of the stack it has checked out,
and git-spice makes that ownership legible
instead of layering a separate lock on top.

This page covers two halves of that model:

- **[Anchors](#anchors)** let many processes share one stack in parallel,
  each scoped to its own region.
- **[Exclusive mode](#exclusive-mode)** lets one process take the whole
  repository to reorganize the stack without contention.

## Anchors

An **anchor** is the branch a worktree is anchored at:
the lower boundary of the region that worktree works on.
Stacks created in the worktree build on top of the anchor,
so `gs repo sync` and restacks in different worktrees
never contend on a single shared trunk checkout.

There are two flavors:

- A **root anchor** is a per-worktree pointer branch that tracks the same
  remote trunk as the main checkout.
  Because it is its own branch, each worktree can update its view of the
  trunk independently.
- An **internal anchor** pins a worktree at an existing tracked branch
  owned by another worktree:
  a dependent worktree, for building on top of work that is still in flight.

### Creating a worktree

Use $$gs anchor create$$ to create a worktree and its anchor in one step.

```freeze language="terminal" float="right"
{green}${reset} gs anchor create ../feat
{green}INF{reset} Created worktree at ../feat
{green}INF{reset} Created anchor feat tracking main
```

By default this creates a root anchor:
a pointer branch named after the worktree directory
(or set with `--name`) that tracks the remote trunk.

```bash
gs anchor create ../feat
```

Pass `-b`/`--branch` to also create and check out a tracked branch
stacked on the anchor, ready to work on:

```bash
gs anchor create ../feat -b feat-login
```

### Dependent worktrees

To build on top of a branch that is still under review in another worktree,
anchor the new worktree on that branch with `--anchor`:

```bash
gs anchor create ../feat-ui --anchor feat-login -b feat-ui
```

The branch named by `--anchor` must already be tracked by git-spice.
The new worktree's stack is based on it,
so syncing the dependency forward flows into the dependent work.

!!! note

    Use `--no-anchor` to skip the anchor entirely and start the worktree
    in detached `HEAD` at the current trunk commit,
    matching plain `git worktree add` behavior.

### Managing worktrees

| Command | Purpose |
|---------|---------|
| $$gs anchor list$$ | List worktrees, their anchors, and the branches stacked in each. |
| $$gs anchor track$$ | Register an existing worktree's anchor branch with git-spice. |
| $$gs anchor rm$$ | Remove a worktree and dissolve its anchor. |

$$gs anchor rm$$ refuses to remove a worktree with uncommitted changes
unless `--force` is given.

## Exclusive mode

Anchors keep parallel processes out of each other's way,
but sometimes you need to step back and reorganize the whole stack:
re-order branches, fold several together, or run an interactive rebase
across the trunk.
Those operations touch branches that other worktrees have checked out,
which Git will refuse.

**Exclusive mode** hands the entire repository to a single process.
$$gs repo park$$ records every linked worktree in a durable manifest
and removes its directory; the branches themselves are left untouched,
so the whole graph stays reachable from the primary checkout.
You reorganize freely, then $$gs repo restore$$ re-creates the worktrees
at their branches' current tips.

```freeze language="terminal"
{green}${reset} gs repo park
{green}INF{reset} Parked worktree ../feat
{green}INF{reset} Parked worktree ../feat-ui
{green}INF{reset} Parked 2 worktree(s); repository is in exclusive mode
{gray}# reorganize the stack here{reset}
{green}${reset} gs repo restore
{green}INF{reset} Restored worktree ../feat
{green}INF{reset} Restored worktree ../feat-ui
{green}INF{reset} Restored 2 worktree(s); exclusive mode cleared
```

The manifest is written **before** any worktree is removed,
and records each worktree's path, branch, and anchor.
If a park or restore is interrupted partway through,
re-running the same command finishes the job:
park resumes from where it stopped,
and restore skips worktrees that already exist.
Nothing is lost to a `Ctrl-C` mid-reorganization.

While the repository is parked, $$gs anchor create$$ is refused —
new worktrees would race with the reorganization in progress.
Run $$gs repo restore$$ first.

!!! warning "Uncommitted changes"

    $$gs repo park$$ refuses to park a worktree with uncommitted changes.
    Commit them first, or pass `--force` to discard them.

### Wrapping a single command

To take exclusive mode for just one command,
use $$gs repo exclusive$$.
It parks the worktrees, runs the command, and always restores them
afterward — even if the command fails.

Separate the command from git-spice's own flags with `--`:

```bash
gs repo exclusive -- git rebase -i main
```

This is the safe way to run a one-off reorganization
without leaving the repository parked if something goes wrong.
