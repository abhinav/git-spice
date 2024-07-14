---
title: Concepts
icon: material/lightbulb
description: >-
  Terminology used throughout the documentation and commands.
hide: [toc]
---

# Concepts

git-spice introduces a few concepts on top of Git.
This section lists a few that are frequently used in the documentation.

```pikchr float="right"
lineht = 0.3in
linewid = 0.4in

up
Main: box "main" fit ht 0.25in fill 0xffffff
dot; line

box "A" same
dot; line
box "B" same fill 0xd0cee0
dot; line lineht/2

ADot: dot invis
left; line
up; line ht lineht/2
box "C" same fill A.fill

move to ADot
right; line
up; line ht lineht/2
box "D" same

dot; line
box "E" same

$padding = 0.1in

US: box thin behind A fill 0xb0fbfd \
  with ne at E.ne+($padding, $padding) \
  wid E.e.x-C.w.x+($padding*2) \
  ht E.n.y-B.y+$padding/2
text "Upstack" with nw at last.nw

DS: box thin behind A fill 0xc49bf9 \
  with nw at (last box.w.x, B.y-$padding/2) \
  wid US.wid \
  ht B.y-A.s.y+$padding/2
text "Downstack" with nw at last.nw-(0,$padding)

S: box thin behind US fill 0xccccfb \
  with nw at US.nw+(-$padding, $padding*2) \
  wid US.wid+$padding*2 \
  ht US.n.y-DS.s.y+$padding*3
text "Stack" with nw at last.nw

text "Current" "branch" with e at (S.w.x-$padding, B.y)
arrow chop thin dashed from last to B

text "Trunk" with w at Main.e

box "F" same as A \
  with nw at $padding east of (S.e, B.ne)
line from 1/2 way between A.n and B.s \
  go right until even with F \
  then to F.s
text "Sibling" with s at F.n
```

**Branch**
:   A regular Git branch.
    Branches can have a *base*: the branch they were created from.
    The branch currently checked out is called the *current branch*.

    In the diagram, `B` is the current branch, and `A` is its base.

**Trunk**
:   The default branch of a repository.
    This is "main" or "master" in most repositories.
    Trunk is the only branch that does not have a base branch.

**Stack**
:   A stack is a collection of branches stacked on top of each other
    in a way that each branch except the trunk has a base branch.

    In the diagram, `A` is stacked on top of trunk,
    `B` is stacked on top of `A`, and so on.
    A branch can have multiple branches stacked on top of it.

**Downstack**
:   Downstack refers to the branches below the current branch,
    all the way to, but not including, the trunk branch.

**Upstack**
:   Upstack refers to the branches stacked on top of the current branch,
    those branches' upstacks, and so on until no more branches remain.
    If a branch has multiple branches stacked on top of it,
    they are both upstack from it.

**Sibling**
:   A sibling to a branch is a branch that shares the same base branch.
    In the diagram, `F` is a sibling to `B`, and `C` is a sibling to `D`.


**Restacking**
:   Restacking is the process of moving a rebasing the contents of a branch
    on top of its base branch, which it may have diverged from.
    This is done to keep the branch up-to-date with its base branch,
    and maintain a linear history.
