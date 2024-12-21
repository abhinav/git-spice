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

## 2024-12-01: Merged downstack history propagation

For: [#331](https://github.com/abhinav/git-spice/issues/331)

Per-branch state now tracks a new field:

```diff
 {
   // ...
+  mergedDownstack: []any, // merged downstack history
 }
```

Any time a branch is merged,
its CR information is appended to with its `mergedDownstack`
and pushed to every branch stacked directly on top of it.
Roughly:

```
onMerge(branch) -> {
  newHistory := append(branch.mergedDownstack, branch.changeID)
  for above := range branch.Aboves() {
    above.mergedDownstack = newHistory
  }
}
```

For example, given:

```
    ┏━□ feat3 (#3)
  ┏━┻□ feat2 (#2)
┏━┻□ feat1 (#1)
main

# merge feat1

  ┏━□ feat3 (#3) |
┏━┻□ feat2 (#2)  | mergedDownstack = [#1]
main

# merge feat2

┏━□ feat3 (#3)   | mergedDownstack = [#1, #2]
main
```

This information is used when generating branch navigation comments
to show merged CRs in the comment.

## 2024-08-04: git-spice configuration will reside in Git configuration

The decision to use `git-config` for git-spice configuration raises the
question whether git-spice configuration will reside in its own file
(that just happens to match Git configuration format)
or whether it will be part of the user's regular Git configuration.

While the former is isolated, it makes for a rougher user experience.
Users either have to edit the file manually or we have to
provide `gs config` commands (which we may do anyway in the future),
as `git config --file=path/to/gs/config` is a bit unwieldy.

On the other hand, if we use regular Git configuration,
besides a familiar path for users to set configuration,
we also get the benefit of Git's configuration hierarchy for free.
Options may be set at system-, user-, repository-, or worktree-level.
The level of flexibility this provides is a good match for more workflows,
and we're able to provide this without adding significant complexity to the UX
to provide similar functionality.

## 2024-08-04: Configuration will use git-config

Thus far, git-spice hasn't provided much in terms of configuration dials.
Behavior is either derived from Git configuration or doesn't have flexibility.
Examples of places where we need configuration include:

- Whether to post a stack visualization comment on PRs.
  Right now, we do this unconditionally.
  We'd like for users to be able to turn this off,
  or have it be posted only of there are at least two branches in the stack.
- Ability to add a prefix to all created branch names--possibly derived
  from an external command.
- Support for custom shorthands in addition to built-ins.

To support this, we'll need a configuration system.
The usual discussions around YAML, TOML, etc. could be had,
but given that Git is a pre-requisite for git-spice,
we can leverage `git-config`.

The following flags can be used for the bulk of the work here.

    --get-regexp <name-pattern>
    --null

Example configuration keys:

- `spice.submit.navigationComment`: true, false, multiple
- `spice.create.branchPrefix`: prefix for new branches
- `spice.alias.*`: custom aliases and shorthands

Note that regardless of configuration system in use,
custom short hands will be special cased:
while most configuration options will have flag-level analogs,
shorthands will not as we expand them before parsing command line flags.

## 2024-05-28: Rebase continuations need a queue

If a re-entrant operation performs several independent interruptible rebases,
we need to store all the commands to run after the conflicts are resolved.
For example:

```go
func branchOnto(branch string, onto string) {
  aboves := findAboves(branch)
  for _, above := range aboves {
    upstackRestack(above, base(branch))
  }
  otherThings()
}
```

If any of the `upstackRestack` operations are interrupted by a conflict,
they will set up a continuation to re-run themselves afterwards.
However, after that runs, we need to continue with the next `upstackRestack`,
and then the rest of `branchOnto`.

Right now, the continuation is a single *set* operation

```go
func upstackRestack(...) {
  if err := rebase(...); err != nil {
    return rebaseRecover(err, ["upstack", "restack"])
    // continuation = ["upstack", "restack"]
  }
}
```

To support multiple continuations, we need to store them in a queue,
which will get appended to as we move up the stack of dependent commands.

```go
func upstackRestack(...) {
  if err := rebase(...); err != nil {
    return rebaseRecover(err, ["upstack", "restack"])
    // continuation = [["upstack", "restack"]]
  }
}

func branchOnto(...) {
  for ... {
    if err := upstackRestack(...); err != nil {
      return rebaseRecover(err, ["branch", "onto"])
      // continuation = [["upstack", "restack"], ["branch", "onto"]]
    }
  }
}
```

This way, we get a queue of commands to run as conflicts are resolved.

This amends the `rebase-continue` file to store a queue of continuations.

    [
      {
        command: []string, // gs command to run
        branch: string?,   // branch to run the command on
      },
      ...
    ]

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
