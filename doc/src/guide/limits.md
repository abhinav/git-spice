---
icon: octicons/stop-16
title: Limitations
description: >-
  Usage constraints and limitations when using git-spice
  to interact with GitHub, GitLab, or Bitbucket Cloud.
---

Usage of git-spice with GitHub, GitLab, and Bitbucket Cloud
runs into limitations of what is possible on those platforms,
and how they handle Git commits.
Some limitations imposed on git-spice are listed below.

## Write access required for stacked CRs

When a branch `F` is stacked on another branch `B`,
and you want to submit Change Requests for both,
the CR for `F` will be created against `B`.
To do this, git-spice needs to push both branches to the same repository.

Therefore, to use git-spice to stack PRs,
you need write access to the repository:
specifically the ability to push new branches.

## Fork mode submits only trunk-based branches

<!-- gs:version unreleased -->

When the upstream and push remotes differ,
git-spice uses fork mode.
In this mode,
branch pushes go to the push remote,
and Change Requests are opened against the upstream remote.

Fork mode creates Change Requests only for branches
that are based directly on trunk.
Branches stacked on top of another local branch are still pushed
to the push remote,
but stack submission commands skip Change Request creation for them.

To submit a fully stacked series of Change Requests,
push access to the upstream repository is still required.

GitHub App authentication is incompatible with Fork mode.
Use one of the other GitHub authentication methods instead.

## Squash-merges restack the upstack

On GitHub, when a Pull Request is squash-merged into the trunk branch,
all commits in that PR are replaced with a single commit with a different hash.
Similarly on GitLab, when fast-forward merges are enabled, and commits are squashed,
the commits in the MR are replaced with a single commit with a different hash.

The branches upstack from that CR are not aware of this new commit,
still referring to the old, unsquashed history of the branch.
GitHub and GitLab do not yet know to reconcile this new commit with the upstack branches,
even though the contents are the same.

```pikchr
linewid = 0.25in

X: [
right
text "A" small; line; text "B" small
line go linewid heading 45; right
text "C" small; line; text "D" small
line go linewid heading 45; right
text "E" small

text "main" with e at A.w
text "feat1" with e at (last.e.x, C.y)
line thin chop dotted from last to C
text "feat2" with e at (last text.e.x, E.y)
line thin chop dotted from last to E
]

Y: [
right
text "A" small; line; text "B" small; line; text "CD" small
move to B.ne
line go linewid heading 45; right
text "C" small; line; text "D" small
line go linewid heading 45; right
text "E" small

text "main" with e at A.w

text "feat2" with e at (last text.e.x, E.y)
line thin chop dotted from last to E
] with n at 0.5in below last.s

Z: [
right
text "A" small; line; text "B" small; line; text "CD" small
line go linewid*2 heading 45; right
text "E" small

text "main" with e at A.w

text "feat2" with e at (last text.e.x, E.y)
line thin chop dotted from last to E
] with n at 0.5in below last.s

arrow " squash-merge" ljust from X.s to Y.n
arrow " restack" ljust from Y.s to Z.n
```

As a result of this, when a branch is squash-merged into the trunk branch,
branches upstack from it need to be restacked, and all their CRs updated.

To restack and re-submit the current stack after a squash merge, you can run:

```freeze language="terminal"
{green}${reset} gs repo sync --restack
{green}${reset} gs stack submit
```

<!-- TODO: can be alleviated somewhat if we implement
     https://github.com/abhinav/git-spice/issues/65 -->

## Bitbucket Cloud limitations

<!-- gs:version v0.25.0 -->

Bitbucket Cloud support has some limitations
compared to GitHub and GitLab:

- **No PR labels**: Bitbucket does not support pull request labels.
  The `--label` flag is ignored.
- **No PR assignees**: Bitbucket does not support pull request assignees.
  The `--assign` flag is ignored.
- **No template enumeration**: Bitbucket does not provide an API
  to list pull request templates.

These are platform limitations, not git-spice limitations.

## Base branch change may dismiss approvals

Some remote repositories are configured to dismiss prior approvals of PRs
when the base branch of that PR is changed.
There is no workaround to this except to reconfigure the repository
as this setting is fundamentally incompatible with a PR stacking workflow.
