---
icon: octicons/stop-16
title: Limitations
description: >-
  Usage constraints and limitations when using git-spice
  to interact with GitHub or GitLab.
---

Usage of git-spice with GitHub and GitLab runs into limitations
of what is possible on those platforms, and how they handle Git commits.
Some limitations imposed on git-spice are listed below.

## Write access required

When a branch `F` is stacked on another branch `B`,
and you want to submit Change Requests for both,
the CR for `F` will be created against `B`.
To do this, git-spice needs to push both branches to the same repository.

Therefore, to use git-spice to stack PRs,
you need write access to the repository:
specifically the ability to push new branches.

## Squash-merges restack the upstack

If a Change Request is squash-merged into the trunk branch,
all commits in that PR are replaced with a single commit with a different hash.

The branches upstack from that PR are not aware of this new commit,
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
branches upstack from it need to be restacked, and all their PRs updated.

<!-- TODO: can be alleviated somewhat if we implement
     https://github.com/abhinav/git-spice/issues/65 -->

## Base branch change may dismiss approvals

Some remote repositories are configured to dismiss prior approvals of PRs
when the base branch of that PR is changed.
There is no workaround to this except to reconfigure the repository
as this setting is fundamentally incompatible with a PR stacking workflow.
