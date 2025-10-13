---
title: FAQ
icon: material/frequently-asked-questions
description: >-
  Frequently asked questions about git-spice,
  a tool to manage stacked branches in Git.
---

# Frequently Asked Questions

![](../img/logo.png){ align=right width=100px }

## What's with the logo?

> *[Xe] who controls the spice controls the universe*

## What is stacking?

Stacking refers to the practice of creating interdependent branches
on top of each other.
Each branch in the stack builds on top of the previous one.
For example, you might have a branch `feature-a` that adds a new feature,
and a branch `feature-b` that builds on top of `feature-a`.

Stacking your changes lets you:

- **Unblock yourself**:
  While one branch is under review, you can start working on the next one.
- **Be kinder to your team**:
  By keeping changes small and focused, you make it easier for your team
  to review, test, and understand your work.
  A 100 line PR gets a more meaningful review than a 1000 line PR.

git-spice helps you manage your stack of branches,
keeping them up-to-date and in sync with each other.

**Related**:

- [The stacking workflow](https://www.stacking.dev/)

## Where is the authentication token stored?

git-spice stores the GitHub authentication in a system-specific secure storage.
See [Authentication > Safety](../setup/auth.md#safety) for details.

## Why doesn't git-spice create one CR per commit?

With tooling like this, there are two options:
each commit is an atomic unit of work, or each branch is.
While the former might be more in line with Git's original philosophy,
the latter is more practical for most teams with GitHub or GitLab-based workflows.

With a PR per commit, when a PR gets review feedback,
you must amend that commit with fixes and force-push.
This is inconvenient for PR reviewers as there's no distinction
between the original changes and those addressing feedback.

However, with a PR per branch, you can keep the original changes separate
from follow-up fixes, even if the branch is force-pushed.
This makes it easier for PR reviewers to work through the changes.

And with squash-merges, you can still get a clean history
consisting of atomic, revertible commits on the trunk branch.

## How does git-spice interact with `rebase.updateRefs`?

The [--update-refs](https://git-scm.com/docs/git-rebase/2.42.1#Documentation/git-rebase.txt---update-refs) flag
and its accompanying
[`rebase.updateRefs`](https://git-scm.com/docs/git-rebase/2.42.1#Documentation/git-rebase.txt-rebaseupdateRefs)
configuration tell `git rebase` to automatically force-update
intermediate branches associated with commits affected by the rebase.
Some use it to help locally manage their stack of branches.

git-spice does not conflict with `--update-refs`.
If you prefer to use `--update-refs` for branch stacking,
you can continue to do so,
while still using git-spice to navigate the stack and submit PRs.
If you run a git-spice restack operation,
it will automatically detect that the branches are already properly stacked,
and leave them as-is.

## Will git-spice add support for other Git hosting services?

As of writing this, git-spice supports GitHub and GitLab.
It is specifically designed to support other forges;
most of the code is forge-agnostic,
with forge-specific code is isolated to their own directories inside
[internal/forge/](https://github.com/abhinav/git-spice/tree/05280813ee113f09ee23529235a585a2388218da/internal/forge).

In fact,

- git-spice's own integration tests run against a simulated forge
  that acts similarly to GitHub and GitLab;
- [GitLab support was added by an external contributor](https://github.com/abhinav/git-spice/pull/477)
  without meaningful changes to the rest of the codebase

Therefore we're confident that adding support for other forges is feasible.

That said,
we do not plan to implement support for additional forges ourselves.

If you would like to see support for a specific forge,
please [open an issue](https://github.com/abhinav/git-spice/issues) signaling your interest.
If you have the time and inclination to contribute,
mention that in the issue
and we will be happy to provide guidance
and work with you to get the contribution merged.
