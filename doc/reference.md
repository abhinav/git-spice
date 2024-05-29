# gs command reference

```
gs <command> [flags]
```

gs (git-spice) is a command line tool for stacking Git branches.

**Global flags**

* `-h`, `--help`: Show help for the command
* `--version`: Print version information and quit
* `-v`, `--verbose`: Enable verbose output
* `-C`, `--dir=DIR`: Change to DIR before doing anything
* `--[no-]prompt`: Whether to prompt for missing information

## gs completion

```
gs completion <shell> [flags]
```

Generate shell completion script

Generates shell completion scripts.
To install the script, add the output of this command to your
shell's rc file.
For example:

	# bash
	gs completion bash >> ~/.bashrc

	# zsh
	gs completion zsh >> ~/.zshrc

	# fish
	gs completion fish >> ~/.config/fish/config.fish

**Arguments**

* `shell`: Shell to generate completions for.

## gs repo init

```
gs repo (r) init (i) [flags]
```

Initialize a repository

Sets up a repository for use.
This isn't strictly necessary to run as most commands will
auto-initialize the repository as needed.

Use the --trunk flag to specify the trunk branch.
This is typically 'main' or 'master',
and picking one is required.

Use the --remote flag to specify the remote to push changes to.
A remote is not required--local stacking will work without it,
but any commands that require a remote will fail.
To add a remote later, re-run this command.

**Flags**

* `--trunk=BRANCH`: Name of the trunk branch
* `--remote=NAME`: Name of the remote to push changes to
* `--reset`: Reset the store if it's already initialized

## gs repo sync

```
gs repo (r) sync (s)
```

Pull latest changes from the remote

Pulls the latest changes from the remote repository.
Deletes branches that have were merged into trunk,
and updates the base branches of branches upstack from those.

## gs stack submit

```
gs stack (s) submit (s) [flags]
```

Submit the current stack

**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `--fill`: Fill in the pull request title and body from the commit messages

## gs stack restack

```
gs stack (s) restack (r)
```

Restack the current stack

## gs upstack restack

```
gs upstack (us) restack (r)
```

Restack this branch those above it

Restacks the current branch and all branches above it
on top of their respective bases.
If multiple branches use another branch as their base,
they will all be restacked on top of the updated base.

Run this command from the trunk branch
to restack all managed branches.

## gs upstack onto

```
gs upstack (us) onto (o) [<onto>] [flags]
```

Move this branch onto another branch

Moves a branch and its upstack branches onto another branch.
Use this to move a complete part of your branch stack to a
different base.

For example,

	# Given:
	#  trunk
	#   └─A
	#     └─B
	#       └─C
	git checkout B
	gs upstack onto main
	# Result:
	#  trunk
	#   ├─A
	#   └─B
	#     └─C

**Arguments**

* `onto`: Destination branch

**Flags**

* `--branch=NAME`: Branch to start at

## gs downstack submit

```
gs downstack (ds) submit (s) [<name>] [flags]
```

Submit the current branch and those below it

Submits Pull Requests for the current branch,
and for all branches below, down to the trunk branch.
Branches that already have open Pull Requests will be updated.

A prompt will allow filling metadata about new Pull Requests.
Use the --fill flag to use the commit messages as-is
and submit without a prompt.

**Arguments**

* `name`: Branch to start at

**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `--fill`: Fill in the pull request title and body from the commit messages

## gs downstack edit

```
gs downstack (ds) edit (e) [<name>] [flags]
```

Edit the order of branches below the current branch

Opens an editor to allow changing the order of branches
from trunk to the current branch.
The branch at the top of the list will be checked out
as the topmost branch in the downstack.
Branches upstack of the current branch will not be modified.
Branches deleted from the list will also not be modified.

**Arguments**

* `name`: Name of the branch to start editing from.

**Flags**

* `--editor=STRING`: Editor to use for editing the downstack.

## gs branch track

```
gs branch (b) track (tr) [<name>] [flags]
```

Track a branch

Use this to track branches created without 'gs branch create',
e.g. with 'git checkout -b' or 'git branch'.

A base will be guessed based on the branch's history.
Use --base to specify a branch explicitly.

**Arguments**

* `name`: Name of the branch to track

**Flags**

* `-b`, `--base=STRING`: Base branch this merges into

## gs branch untrack

```
gs branch (b) untrack (untr) [<name>]
```

Forget a tracked branch

Removes information about a tracked branch,
without deleting the branch itself.
If the branch has any branches upstack from it,
they will be updated to point to its base branch.

**Arguments**

* `name`: Name of the branch to untrack

## gs branch checkout

```
gs branch (b) checkout (co) [<name>]
```

Switch to a branch

**Arguments**

* `name`: Name of the branch to delete

## gs branch onto

```
gs branch (b) onto (on) [<onto>] [flags]
```

Move a branch onto another branch

Transplants the commits of a branch on top of another branch
leaving the rest of the branch stack untouched.
Use this to extract a single branch from an otherwise unrelated
branch stack.

For example,

	# Given:
	#  trunk
	#   └─A
	#     └─B
	#       └─C
	git checkout B
	gs branch onto main
	# Result:
	#  trunk
	#   ├─B
	#   └─A
	#     └─C

**Arguments**

* `onto`: Destination branch

**Flags**

* `--branch=NAME`: Branch to move

## gs branch create

```
gs branch (b) create (c) [<name>] [flags]
```

Create a new branch

Creates a new branch containing the staged changes
on top of the current branch.
If there are no staged changes, creates an empty commit.

By default, the new branch is created on top of the current branch,
but it does not affect the rest of the stack.
Use the --insert flag to restack all existing upstack branches
on top of the new branch.
For example,

	# Given:
	#
	#  trunk
	#   └─A
	#     └─B
	#       └─C
	git checkout A
	gs branch create --insert X
	# Result:
	#
	#  trunk
	#   └─A
	#     └─X
	#       └─B
	#         └─C

Instead of --insert,
you can use --below to place the new branch
below the current branch.
This is equivalent to checking out the base branch
and creating a new branch with --insert there.

	# Given:
	#
	#  trunk
	#   └─A
	#     └─B
	#       └─C
	git checkout A
	gs branch create --below X
	# Result:
	#
	#  trunk
	#   └─X
	#     └─A
	#       └─B
	#         └─C

**Arguments**

* `name`: Name of the new branch

**Flags**

* `--insert`: Restack the upstack of the current branch on top of the new branch
* `--below`: Place the branch below the current branch. Implies --insert.
* `-m`, `--message=STRING`: Commit message

## gs branch delete

```
gs branch (b) delete (rm) [<name>] [flags]
```

Delete a branch

Deletes the specified branch and removes its changes from the
stack. Branches above the deleted branch are rebased onto the
branch's base.

If a branch name is not provided, an interactive prompt will be
shown to pick one.

**Arguments**

* `name`: Name of the branch to delete

**Flags**

* `-f`, `--force`: Force deletion of the branch

## gs branch fold

```
gs branch (b) fold (fo) [<name>]
```

Merge a branch into its base

Merges the changes of a branch into its base branch
and deletes it.
Branches above the folded branch will be restacked
on top of the base branch.

**Arguments**

* `name`: Name of the branch

## gs branch edit

```
gs branch (b) edit (e)
```

Edit the commits in a branch

Begins an interactive rebase of a branch without affecting its
base branch. This allows you to edit the commits in the branch,
reword their messages, etc.
After the rebase, the branches upstack from the edited branch
will be restacked.

## gs branch rename

```
gs branch (b) rename (mv) [<name>]
```

Rename a branch

Renames a tracked branch, updating internal references to it.

If you renamed a branch without using this command,
track the new branch name with 'gs branch track',
and untrack the old name with 'gs branch untrack'.

**Arguments**

* `name`: New name of the branch

## gs branch restack

```
gs branch (b) restack (r) [<name>]
```

Restack a branch

Updates a branch after its base branch has been changed,
rebasing its commits on top of the base.

**Arguments**

* `name`: Branch to restack

## gs branch submit

```
gs branch (b) submit (s) [<name>] [flags]
```

Submit a branch

Creates or updates a Pull Request for the specified branch,
or the current branch if none is specified.
The Pull Request will use the tracked base branch
as the merge base.

For new Pull Requests, a prompt will allow filling metadata.
Use the --title and --body flags to set the title and body,
or the --fill flag to use the commit message to fill them in.

**Arguments**

* `name`: Branch to submit

**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `--title=STRING`: Title of the pull request (if creating one)
* `--body=STRING`: Body of the pull request (if creating one)
* `--draft`: Mark the pull request as a draft
* `--fill`: Fill in the pull request title and body from the commit messages

## gs commit create

```
gs commit (c) create (c) [flags]
```

Create a new commit

Commits the staged changes to the current branch,
restacking upstack branches if necessary.
Use this to keep upstack branches in sync
as you update a branch in the middle of the stack.

**Flags**

* `-a`, `--all`: Stage all changes before committing.
* `-m`, `--message=STRING`: Use the given message as the commit message.

## gs commit amend

```
gs commit (c) amend (a) [flags]
```

Amend the current commit

Amends the last commit with the staged changes,
restacking upstack branches if necessary.
Use this to keep upstack branches in sync
as you update a branch in the middle of the stack.

**Flags**

* `-m`, `--message=STRING`: Use the given message as the commit message.
* `-n`, `--no-edit`: Don't edit the commit message

## gs rebase continue

```
gs rebase (rb) continue (c)
```

Continue an interrupted operation

Continues an ongoing git-spice operation interrupted by
a git rebase after all conflicts have been resolved.
For example, if 'gs upstack restack' gets interrupted
because a conflict arises during the rebase,
you can resolve the conflict and run 'gs rebase continue'
(or its shorthand 'gs rbc') to continue the operation.

The command can be used in place of 'git rebase --continue'
even if a git-spice operation is not currently in progress.

## gs rebase abort

```
gs rebase (rb) abort (a)
```

Abort an operation

Cancels an ongoing git-spice operation that was interrupted by
a git rebase.
For example, if 'gs upstack restack' encounters a conflict,
cancel the operation with 'gs rebase abort'
(or its shorthand 'gs rba'),
going back to the state before the rebase.

The command can be used in place of 'git rebase --abort'
even if a git-spice operation is not currently in progress.

## gs up

```
gs up (u) [<n>] [flags]
```

Move up one branch

Moves up the stack to the branch on top of the current one.
If there are multiple branches with the current branch as base,
you will be prompted to pick one.

**Arguments**

* `n`: Number of branches to move up.

## gs down

```
gs down (d) [<n>] [flags]
```

Move down one branch

Moves down the stack to the branch below the current branch.
As a convenience,
if the current branch is at the bottom of the stack,
this command will move to the trunk branch.

**Arguments**

* `n`: Number of branches to move up.

## gs top

```
gs top (U) [flags]
```

Move to the top of the stack

Jumps to the top-most branch in the current branch's stack.
If there are multiple top-most branches,
you will be prompted to pick one.

## gs bottom

```
gs bottom (D) [flags]
```

Move to the bottom of the stack

Jumps to the bottom-most branch below the current branch.
This is the branch just above the trunk.

## gs trunk

```
gs trunk [flags]
```

Move to the trunk branch

