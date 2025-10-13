---
title: Local stacks
icon: octicons/git-branch-16
description: >-
  Manage, navigate, and manipulate stacks of branches with git-spice.
---

# Local development with branch stacks

## Stacking branches

Starting at the trunk branch, any number of branches may be stacked on top.
git-spice learns about the relationships between branches by 'tracking' them
in an [internal data store](internals.md).
Branches may be tracked manually or automatically:

- [**Automatic stacking**](#automatic-stacking) with $$gs branch create$$
- [**Manual stacking**](#manual-stacking) with $$gs branch track$$

### Automatic stacking

```freeze language="terminal" float="right"
{green}${reset} $EDITOR file.txt
{gray}# make your changes{reset}
{green}${reset} git add file.txt
{green}${reset} gs branch create feat1
```

The preferred way to create and track a stacked branch is with
$$gs branch create$$. The steps are simple:

1. Modify your code and prepare your changes to be committed with `git add`.
2. Run $$gs branch create$$.

This will create a new branch stacked on top of the current branch,
commit the staged files to it, and track it with the current branch as base.
An editor will open to let you write a commit message
if one was not provided with the `-m`/`--message` flag.

??? tip "But I use `git commit -a`"

    If you prefer to use `git commit -a` to automatically stage files
    before committing, use `gs branch create -a` to do the same with git-spice.

    Explore the full list of options at $$gs branch create$$.

??? info "Creating branches without committing"

    If you prefer a workflow where you create branches first
    and then work on them,
    you can configure git-spice to never commit by default.
    See [Create branches without committing](../community/recipes.md#create-branches-without-committing).

### Manual stacking

git-spice does not require to change your workflow too drastically.
If you prefer to use your usual workflow to create branches and commit changes,
use $$gs branch track$$ to inform git-spice of the branch after creating it.

```freeze language="terminal" float="right"
{green}${reset} git checkout -b feat1
{gray}# make your changes{reset}
{green}${reset} git commit
{green}${reset} gs branch track
{green}INF{reset} feat1: tracking with base main
```

For example, you may:

1. Create a new branch with `git checkout -b`.
2. Make changes and commit them with `git commit`.
3. Run $$gs branch track$$ to track the branch.

The $$gs branch track$$ command automatically guesses the base branch
for the newly tracked branch.
Use the `--base` option to set it manually.

#### Tracking multiple branches at once

<!-- gs:version unreleased -->

If you manually created a stack of branches,
you can track them all at once with $$gs downstack track$$.
This command traverses the commit graph downwards from the current branch,
identifying other branches that need to be tracked along the way.

```freeze language="terminal"
{green}${reset} gs downstack track
Track fire with base: air
Track air with base: earth
Track earth with base:

▶ {yellow}water{reset}

  None of these
```

The above would be similar to running:

```freeze language="terminal"
{green}${reset} gs branch track {yellow}earth{reset} --base {cyan}water{reset}
{green}${reset} gs branch track {yellow}air{reset}   --base {cyan}earth{reset}
{green}${reset} gs branch track {yellow}fire{reset}  --base {cyan}air{reset}
```

## Naming branches

We advise picking descriptive names for branches.
You don't need to remember the exact names as git-spice provides
a number of utilities to help navigate and manipulate the stack.
Most places where a branch name is required provide
[shell completion](../setup/shell.md) and fuzzy-searchable lists.

!!! tip "Can't think of a branch name?"

    If you can't think of a name for a branch,
    run $$gs branch create$$ without any arguments.
    git-spice will use the commit message to generate a branch name for you.

## Navigating the stack

git-spice offers the following commands to navigate within a stack of branches:

<div class="grid" markdown>

<div markdown>
* $$gs down$$ checks out the branch below the current branch
* $$gs up$$ checks out a branch stacked on top of the current branch,
  prompting to pick one if there are multiple
* $$gs bottom$$ moves to the bottommost branch in the stack,
  right above the trunk
* $$gs top$$ moves to the topmost branch in the stack,
  prompting to pick one if there are multiple
* $$gs trunk$$ checks out the trunk branch
* $$gs branch checkout$$ checks out any branch in the repository
</div>

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

color = gray
linerad = 0.05in

arrow chop thin \
  from B go left 0.3in \
  then down until even with Main \
  then to Main
text "gs trunk " mono rjust \
 at 0.3in west of 1/2 way between B and Main

arrow chop thin \
  from B go right 0.3in \
  then down until even with A \
  then to A
text " gs down" mono ljust \
 at 0.3in east of 1/2 way between A and B

arrow behind Main chop thin \
  from B up until even with C \
  then to C
arrow behind Main chop thin \
  from B up until even with D \
  then to D
text "gs up" mono at 0.1in north of 1/2 way between C and D

arrow chop thin \
  from B go right until even with 0.1in east of E.e \
  then up until even with E \
  then to E
text " gs top" mono ljust \
  at (0.1in east of E.e.x, B.y+(E.y-B.y)/2)

arrow chop thin \
  from B go left until even with 0.1in west of C.w \
  then up until even with C \
  then to C
text "gs top " mono rjust \
  at (0.1in west of C.w.x, B.y+(C.y-B.y)/2)
```

</div>

These commands allow for relative movement within the stack,
so that you don't have to remember exact branch names
or their positions in the stack.

!!! question "What's the value of `gs branch checkout`?"

    ```freeze language="terminal" float="right"
    {green}${reset} gs branch checkout my-feature
    ```

    You may be wondering about $$gs branch checkout$$
    when `git checkout` exists.
    The command behaves not much differently than `git checkout`
    when provided with a branch name as an argument.

    ```freeze language="terminal" float="right"
    {green}${reset} gs branch checkout {gray}# or gs bco{reset}
    {green}Select a branch to checkout{reset}:
    ┏━■ {yellow}docs ◀{reset}
    ┃ ┏━□ github-optimization
    ┣━┻□ github-support
    main
    ```

    Its real value lies in the interactive mode.
    Invoke it without arguments to get a fuzzy-searchable list of branches,
    visualized as a tree-like structure to help you navigate the stack.

## Committing and restacking

With a stacked branch checked out,
you can commit to it as usual with `git commit`, `git commit --amend`, etc.
However, after committing, the branches upstack from the current branch
need to be restacked to maintain a linear history.
Use $$gs upstack restack$$ to do this.

<div class="grid" markdown>

```freeze language="terminal"
{green}${reset} gs branch checkout feat1
{green}${reset} $EDITOR file.txt
{gray}# prepare your changes{reset}
{green}${reset} git commit
{green}${reset} gs upstack restack
```

```pikchr
linewid = 0.25in

X: [
  right
  text "A" small; line; text "B" small;
  line go linewid heading 45; right; text "C" small

  text "feat1" with e at A.w
  text "feat2" with e at (last text.e.x, C.y)
  line thin chop dotted from last to C
]

Y: [
  right
  text "A" small; line; text "B" small; line; text "D" small
  move to B.ne;
  line go linewid heading 45; right;
  text "C" small

  text "feat1" with e at A.w
  text "feat2" with e at (last text.e.x, C.y)
  line thin chop dotted from last to C
] with nw at 0.5in south of last.sw

Z: [
  right
  text "A" small; line; text "B" small; line; text "D" small
  line go linewid heading 45; right; text "C" small

  text "feat1" with e at A.w
  text "feat2" with e at (last text.e.x, C.y)
  line thin chop dotted from last to C
] with nw at 0.5in south of last.sw

arrow color gray from X.s down until even with Y.n
text "git commit" mono with w at last.e

arrow color gray from Y.s down until even with Z.n
text "gs upstack restack" mono with w at last.e
```

</div>

!!! tip

    git-spice also offers
    $$gs branch restack$$ to restack just the current branch onto its base,
    and $$gs stack restack$$ to restack all branches in the current stack.

### Automatic restacking

git-spice provides several convenience commands
that run common Git operations and automatically restack upstack branches.

These include but are not limited to:

- $$gs commit create$$ (or $$gs commit create|gs cc$$)
  commits changes to the current branch and restacks upstack branches
- $$gs commit amend$$ (or $$gs commit amend|gs ca$$)
  amends the last commit and restacks upstack branches
- $$gs commit split$$ (or $$gs commit split|gs csp$$)
  interactively splits the last commit into two and restacks upstack branches

For example, the interaction above can be shortened to:

```freeze language="terminal"
{green}${reset} gs branch checkout feat1
{green}${reset} $EDITOR file.txt
{gray}# prepare your changes{reset}
{green}${reset} gs commit create {gray}# or gs cc{reset}
```

### Editing commits in a branch

The $$gs branch edit$$ command starts a `git rebase --interactive`
for the commits in the current branch.

```freeze language="terminal" float="right"
{green}${reset} gs branch edit
```

```
pick c0d0855d feat: Add support for things
pick 78c047c5 doc: Document something
pick 4dbf01a5 fix: Fix a thing that was broken
```

Use the usual Git rebase commands to edit, reorder, squash, or split commits.

After the rebase operation completes successfully,
upstack branches will be restacked on top of the current branch.

#### Handling rebase interruptions

```freeze language="terminal" float="right"
{green}${reset} gs rebase continue
{green}${reset} gs rebase abort
```

If a rebase operation is interrupted due to a conflict,
or because an `edit` or `break` instruction was used in the rebase script,
git-spice will pause execution and let you resolve the issue.
You may then:

- Resolve the conflict, make any planned changes,
  and run $$gs rebase continue$$ (`gs rbc` for short)
  to let git-spice continue the rest of the operation; or
- Run $$gs rebase abort$$ (`gs rba` for short) to abort the operation
  and go back to the state before the rebase started.

### Squashing commits in a branch

<!-- gs:version v0.11.0 -->

```freeze language="terminal" float="right"
{green}${reset} gs branch squash
```

$$gs branch squash$$ will squash all commits in the current branch
and open an editor to let you write a commit message for the squashed commit.
Use the `-m`/`--message` flag to provide a message without opening an editor.

If you want to squash only a subset of commits in the branch,
use $$gs branch edit$$ and add `squash` or `fixup` commands.


## Inserting into the stack

By default, $$gs branch create$$ creates a branch
stacked on top of the current branch.
It does not manipulate the existing stack in any way.

If you're in the middle of the stack, use the `--insert` option
to stack the new branch between the current branch and its upstack branches.

=== "With `--insert`"

    <div class="grid" markdown>

    ```freeze language="terminal"
    {green}${reset} gs branch checkout feat1
    {green}${reset} gs branch create --insert feat4
    ```

    ```pikchr
    linerad = 0.05in

    X: [
      up; text "feat1"; F1: dot invis
      line go up 0.15in then go right 0.15in
      text "feat2"
      line from F1 go up 0.3in then go right 0.15in
      text "feat3"
    ]


    Y: [
      up; text "feat1"; F1: dot invis
      line go up 0.15in then go right 0.15in
      text "feat4"; up; F2: dot invis
      line from F2 go up 0.15in then go right 0.15in
      text "feat2"
      line from F2 go up 0.30in then go right 0.15in
      text "feat3"
    ] with sw at 0.5in east of last.se

    arrow color gray from X.e go right until even with Y.w
    ```

    </div>

=== "Without `--insert`"

    <div class="grid" markdown>

    ```freeze language="terminal"
    {green}${reset} gs branch checkout feat1
    {green}${reset} gs branch create feat4
    ```

    ```pikchr
    linerad = 0.05in

    X: [
      up; text "feat1"; F1: dot invis
      line go up 0.15in then go right 0.15in
      text "feat2"
      line from F1 go up 0.3in then go right 0.15in
      text "feat3"
    ]


    Y: [
      up; text "feat1"; F1: dot invis
      line go up 0.15in then go right 0.15in
      text "feat2"
      line from F1 go up 0.3in then go right 0.15in
      text "feat3"
      line from F1 go up 0.45in then go right 0.15in
      text "feat4"
    ] with sw at 0.5in east of last.se

    arrow color gray from X.e go right until even with Y.w
    ```

    </div>

## Splitting the stack

Use $$gs branch split$$ to split a branch with one or more commits
into two or more branches.

The command presents a prompt allowing you to select one or more
*split points* in the branch's history.
After selecting the split points,
it will prompt you for a name for each new branch.

```freeze language="terminal"
{green}${reset} gs branch split
{green}Select commits{reset}:
{yellow}▶   c4fb996{reset} Add an aardvark {gray}(12 minutes ago){reset}
{yellow}    f1d2d2f{reset} Refactor the pangolin {gray}(5 minutes ago){reset}
  ■ {yellow}4186a53{reset} Invent penguins {gray}(1 second ago){reset} [penguin]
  Done
```

!!! tip

    You can use $$gs commit split$$ and $$gs branch edit$$
    to safely and easily manipulate the commits in the branch
    before splitting it into multiple branches.

<!-- gs:version v0.8.0 -->
If you split a branch after submitting it for review,
git-spice will prompt you to assign the submitted CR
to one of the branches.

```freeze language="ansi"
--8<-- "captures/branch-split-reassociate.txt"
```

<!-- gs:version v0.18.0 -->
When prompted for branch names during a split,
you can reuse the original branch name for one of the intermediate commits.
When you do this, the original branch will be reassigned to that commit,
preserving its metadata (such as change requests),
and you'll be prompted to provide a new name for the remaining HEAD commits.
This gives you more control over which commits retain the original branch's identity.

### Splitting non-interactively

```freeze language="terminal" float="right"
{green}${reset} gs branch split {gray}\{reset}
    --at HEAD~2:aardvark {gray}\{reset}
    --at HEAD~1:pangolin
```

If the interactive prompt is not suitable for your workflow,
you can also split a branch non-interactively
by specifying the split points with the `--at` option
one or more times.

The option takes the form:

```
--at COMMIT:NAME
```

Where `COMMIT` is a reference to a commit in the branch's history,
and `NAME` is the name of the new branch.

If running in non-interactive mode
and the branch has already been submitted for review,
it will be left assigned to the original branch.

## Moving branches around

Use the $$gs upstack onto$$ command to move a branch onto another base branch,
and bring its upstack branches along with it.

<div class="grid" markdown>

```freeze language="terminal"
{green}${reset} gs branch checkout feat2
{green}${reset} gs upstack onto main
```

```pikchr
linerad = 0.05in
lineht = 0.15in
linewid = 0.15in

X: [
  text "main"; up;
  line go up then go right
  text "feat1"; up;
  line go up then go right
  text "feat2"; up;
  line go up then go right
  text "feat3"
]

Y: [
  text "main"; up; M: dot invis
  line go up then go right
  text "feat1"; up;
  move to M
  line go up lineht*2.5 then go right
  text "feat2"; up;
  line go up then go right
  text "feat3"
] with sw at 0.5in east of last.se

color = gray
arrow from X.e go right until even with Y.w
```

</div>

!!! tip

    Omit the target branch name
    to get a list of branches to choose from.

This is useful when you realize that a branch
does not actually depend on the base branch it is stacked on.
With this command, you can move an entire section of the stack
down to a different base branch, or even to the trunk branch,
reducing the number of changes that need to merge before the target branch.

### Moving only the current branch

In rarer cases, you want to extract the current branch from the stack
and move it to a different base branch,
while leaving the upstack branches where they are.

Use $$gs branch onto$$ for this purpose.

<div class="grid" markdown>

```freeze language="terminal"
{green}${reset} gs branch checkout feat2
{green}${reset} gs branch onto main
```

```pikchr
linerad = 0.05in
lineht = 0.15in
linewid = 0.15in

X: [
  text "main"; up;
  line go up then go right
  text "feat1"; up;
  line go up then go right
  text "feat2"; up;
  line go up then go right
  text "feat3"
]

Y: [
  text "main"; up; M: dot invis
  line go up then go right
  text "feat1"; up;
  line go up then go right
  text "feat3"; up
  move to M
  line go up lineht*4 then go right
  text "feat2"
] with sw at 0.5in east of last.se

color = gray
arrow from X.e go right until even with Y.w
```

</div>

This is most useful when you find that a branch is completely independent
of other changes in the stack.

## Removing branches from the stack

Use $$gs branch delete$$ to remove a branch from the stack
and delete it from the repository.
Branches that are upstack from the deleted branch
will be restacked on top of the deleted branch's original base branch.

<div class="grid" markdown>

```freeze language="terminal"
{green}${reset} gs branch delete feat2
{green}INF{reset} feat2: deleted (was 644a286)
```

```pikchr
linerad = 0.05in
lineht = 0.15in
linewid = 0.15in

X: [
  text "main"; up;
  line go up then go right
  text "feat1"; up;
  line go up then go right
  text "feat2"; up;
  line go up then go right
  text "feat3"
]

Y: [
  text "main"; up;
  line go up then go right
  text "feat1"; up;
  line go up then go right
  text "feat3"
] with sw at 0.5in east of last.se

color = gray
arrow from X.e go right until even with Y.w
```

</div>

!!! tip

    Omit the target branch name
    to get a list of branches to choose from.

### Removing a branch without deleting it

```freeze language="terminal" float="left"
{green}${reset} gs branch untrack feat2
```

If you want to remove a branch from the stack
but don't want to delete the branch from the repository,
use the $$gs branch untrack$$ command.
