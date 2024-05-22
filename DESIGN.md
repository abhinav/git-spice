# Design Decision Log

<!--
    This section tracks design decisions over time.
    Add new entries to the top in the form:

        ### YYYY-MM-DD: Title

        Details

    If a decision changes, don't go back in time to change it.
    Add a new entry explaining the change,
    and add a link from the old entry to the new one.
-->

## 2024-05-27: Continuing operations with `gs rebase continue`

A number of git-spice commands run `git rebase` under the hood.
These rebase operations can be interrupted by conflicts, or
for interactive rebases, by the user adding an `edit` or `break` instruction.

We offer a `gs rebase continue` command to resume the interrupted operation.
For this, we need to track the "continuation command":
the command that must be run after the conflict is resolved.

Different commands have different continuation commands:

- `branch restack`:
  Re-run the original command.
  This will verify that the branch is restacked and update internal state.
- `stack restack`, `upstack restack`, `downstack restack`:
  Re-run the original command.
  This will skip branches that are already restacked,
  and continue restacking the remaining branches.
- `branch onto`:
  Re-run the original command.
  This will verify that the branch was moved, and update internal state.
- `branch edit`: Run `upstack restack`.

All but `branch edit` re-run the original command to continue,
but this divergence means we have to allow for something other than
"re-run the original command."

For this, we can track a new file in the git spice state: `rebase-continue`.
If this file exists, it will contain:

    {
      command: []string, // gs command to run
      branch: string?,   // branch to run the command on
    }

`gs rebase continue` will check out `$branch` and run `gs ${args}`
in a loop until the file doesn't exist.

## 2024-05-18: Branch state tracks upstream branch name

It's possible for a branch to be renamed locally after a `gs branch submit`.
In such a case, we should still push to the original remote branch
instead of creating a new remote branch and pull request.
To make this possible, we'll track the upstream branch name
in the per-branch state.

This amends the per-branch files in git-spice state to include:

```diff
 {
   // ...
+  upstream: string?, // upstream branch name
 }
```

## 2024-05-18: Branch state tracks PR number in a GitHub section

Instead of tracking the PR number in a top-level field,
we're moving it to a `github` section in the per-branch state.
This leaves room for non-GitHub integrations in the future.

This amends the per-branch files in git-spice state:

```diff
 {
     // ...
-    pr: int?,
+    github: {
+        pr: int?,
+    },
 }
```

## 2024-05-02: Repository state tracks remote name

We won't assume that the remote name is always `origin`.
We'll let the user pick one and track it in the repository-level state
alongside the trunk branch name.

The remote name will be optional: if not set,
git-spice can still be used to manage and stack branches locally.
A remote name is only needed for operations that push or pull.

This amends the `repo` file in git-spice state to include:

```diff
 {
   // ...
+  remote: string?, // remote name (if any)
 }
```

## 2024-04-02: Relative navigation commands are top-level

Relative navigation commands move between branches in the stack:
up, down, top, and bottom.
Their scope is not necessarily limited to a single branch,
so they don't fit well under the `branch` noun.

More importantly, they're intended to be used very frequently,
so it makes sense to have them available as top-level commands.

> NOTE:
> Decisions prior to this point don't have a date
> because they were made before the decision log was created
> or aren't tied to a specific date.

## 202X-XX-XX: Tracking git-spice state in a local Git ref

<a id="init-state"></a>

State required by git-spice will be tracked in a local Git ref.
The ref will point to a *commit object*, which tracks a tree
holding state for every tracked Git branch,
and any requisite repository-level information.

Each branch will be stored as a JSON object (probably)
with the following state.

    {
        base: {
          name: string, // base branch name
          hash: string, // base branch tip hash
        },
        pr: int?,     // pull request number
    }

Repository-level state will include at least:

    {
      trunk: string, // main branch name
    }

Possible example layout:

    repo            // repository information
    branches/
      feature1
      user1/feature2
      <branch-name> // branch information

Choices worth highlighting:

- The Git ref for git-spice state points to a commit object, not a tree.
  This will give us a historical operation log over time,
  should that ever become a command worth exposing.
- Branches are tracked as entries inside the same ref
  instead of ref-per-branch (e.g. `refs/gs/branches/$branch`),
  even at the cost of implementation complexity.
  This has the advantage of not polluting .git with excessive noise.

## 202X-XX-XX: Noun-verb CLI command structure

The CLI will offer commands in the form:

    gs [noun] [verb]

For example:

    gs stack submit
    gs branch create feature1
    gs branch checkout feature1
    gs commit create
    gs commit amend

This structure lends itself well to memorable short-hand aliases for commands.
For example, the above commands could be aliased as:

    gs ss
    gs bc feature1
    gs bco feature1
    gs cc
    gs ca

While it's possible to move some of the subcommands to top-level commands,
it's easier to remember them by a noun defining the scope of the command.
