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

**Configuration**: [spice.forge.github.apiUrl](/cli/config.md#spiceforgegithubapiurl), [spice.forge.github.url](/cli/config.md#spiceforgegithuburl), [spice.forge.gitlab.oauth.clientID](/cli/config.md#spiceforgegitlaboauthclientid), [spice.forge.gitlab.url](/cli/config.md#spiceforgegitlaburl)

## Shell

### gs shell completion

```
gs shell completion [<shell>]
```

Generate shell completion script

To set up shell completion, eval the output of this command
from your shell's rc file.
For example:

	# bash
	eval "$(gs shell completion bash)"

	# zsh
	eval "$(gs shell completion zsh)"

	# fish
	eval "$(gs shell completion fish)"

If shell name is not provided, the current shell is guessed
using a heuristic.

**Arguments**

* `shell`: Shell to generate completions for.

## Authentication

### gs auth login

```
gs auth login [flags]
```

Log in to a service

For GitHub, a prompt will allow selecting between
OAuth, GitHub App, and Personal Access Token-based authentication.
The differences between them are explained in the prompt.

The authentication token is stored in a system-provided secure storage.
Use 'gs auth logout' to log out and delete the token from storage.

Fails if already logged in.
Use --refresh to force a refresh of the authentication token,
or change the authentication method.

**Flags**

* `--refresh`: Force a refresh of the authentication token

### gs auth status

```
gs auth status [flags]
```

Show current login status

Exits with a non-zero code if not logged in.

### gs auth logout

```
gs auth logout [flags]
```

Log out of a service

The stored authentication information is deleted from secure storage.
Use 'gs auth login' to log in again.

No-op if not logged in.

## Repository

### gs repo init

```
gs repo (r) init (i) [flags]
```

Initialize a repository

A trunk branch is required.
This is the branch that changes will be merged into.
A prompt will ask for one if not provided with --trunk.

Most branch stacking operations are local
and do not require a network connection.
For operations that push or pull commits, a remote is required.
A prompt will ask for one during initialization
if not provided with --remote.

Re-run the command on an already initialized repository
to change the trunk or remote.
If the trunk branch is changed on re-initialization,
existing branches stacked on the old trunk
will be updated to point to the new trunk.

Re-run with --reset to discard all stored information
and untrack all branches.

**Flags**

* `--trunk=BRANCH`: Name of the trunk branch
* `--remote=NAME`: Name of the remote to push changes to
* `--reset`: Forget all information about the repository

### gs repo sync

```
gs repo (r) sync (s) [flags]
```

Pull latest changes from the remote

Branches with merged Change Requests
will be deleted after syncing.

The repository must have a remote associated for syncing.
A prompt will ask for one if the repository
was not initialized with a remote.

**Flags**

* `--restack`: Restack the current stack after syncing

## Log

### gs log short

```
gs log (l) short (s) [flags]
```

List branches

Only branches that are upstack and downstack from the current
branch are shown.
Use with the -a/--all flag to show all tracked branches.

**Flags**

* `-a`, `--all` ([:material-wrench:{ .middle title="spice.log.all" }](/cli/config.md#spicelogall)): Show all tracked branches, not just the current stack.

**Configuration**: [spice.log.all](/cli/config.md#spicelogall), [spice.log.crFormat](/cli/config.md#spicelogcrformat)

### gs log long

```
gs log (l) long (l) [flags]
```

List branches and commits

Only branches that are upstack and downstack from the current
branch are shown.
Use with the -a/--all flag to show all tracked branches.

**Flags**

* `-a`, `--all` ([:material-wrench:{ .middle title="spice.log.all" }](/cli/config.md#spicelogall)): Show all tracked branches, not just the current stack.

**Configuration**: [spice.log.all](/cli/config.md#spicelogall), [spice.log.crFormat](/cli/config.md#spicelogcrformat)

## Stack

### gs stack submit

```
gs stack (s) submit (s) [flags]
```

Submit a stack

Change Requests are created or updated
for all branches in the current stack.

Use --dry-run to print what would be submitted without submitting it.
For new Change Requests, a prompt will allow filling metadata.
Use --fill to populate title and body from the commit messages,
and --[no-]draft to set the draft status.
Omitting the draft flag will leave the status unchanged of open CRs.

Use --no-publish to push branches without creating CRs.
This has no effect if a branch already has an open CR.
Use --update-only to only update branches with existing CRs,
and skip those that would create new CRs.

Use --nav-comment=false to disable navigation comments in CRs,
or --nav-comment=multiple to post those comments only if there are multiple CRs in the stack.


**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `-c`, `--fill`: Fill in the change title and body from the commit messages
* `--[no-]draft`: Whether to mark change requests as drafts
* `--[no-]publish` ([:material-wrench:{ .middle title="spice.submit.publish" }](/cli/config.md#spicesubmitpublish)): Whether to create CRs for pushed branches. Defaults to true.
* `-w`, `--[no-]web` ([:material-wrench:{ .middle title="spice.submit.web" }](/cli/config.md#spicesubmitweb)): Open submitted changes in a web browser
* `--nav-comment=true` ([:material-wrench:{ .middle title="spice.submit.navigationComment" }](/cli/config.md#spicesubmitnavigationcomment)): Whether to add a navigation comment to the change request. Must be one of: true, false, multiple.
* `--force`: Force push, bypassing safety checks
* `-u`, `--update-only`: Only update existing change requests, do not create new ones

**Configuration**: [spice.submit.navigationComment](/cli/config.md#spicesubmitnavigationcomment), [spice.submit.publish](/cli/config.md#spicesubmitpublish), [spice.submit.web](/cli/config.md#spicesubmitweb)

### gs stack restack

```
gs stack (s) restack (r)
```

Restack a stack

All branches in the current stack are rebased on top of their
respective bases, ensuring a linear history.

### gs stack edit

```
gs stack (s) edit (e) [flags]
```

Edit the order of branches in a stack

This operation requires a linear stack:
no branch can have multiple branches above it.

An editor opens with a list of branches in the current stack in-order,
with the topmost branch at the top of the file,
and the branch closest to the trunk at the bottom.

Modifications to the list will be reflected in the stack
when the editor is closed.
If the file is cleared, no changes will be made.
Branches that are deleted from the list will be ignored.

**Flags**

* `--editor=STRING`: Editor to use for editing the downstack. Defaults to Git's default editor.
* `--branch=NAME`: Branch whose stack we're editing. Defaults to current branch.

### gs upstack submit

```
gs upstack (us) submit (s) [flags]
```

Submit a branch and those above it

Change Requests are created or updated
for the current branch and all branches upstack from it.
If the base of the current branch is not trunk,
it must have already been submitted by a prior command.
Use --branch to start at a different branch.

Use --dry-run to print what would be submitted without submitting it.
For new Change Requests, a prompt will allow filling metadata.
Use --fill to populate title and body from the commit messages,
and --[no-]draft to set the draft status.
Omitting the draft flag will leave the status unchanged of open CRs.

Use --no-publish to push branches without creating CRs.
This has no effect if a branch already has an open CR.
Use --update-only to only update branches with existing CRs,
and skip those that would create new CRs.

Use --nav-comment=false to disable navigation comments in CRs,
or --nav-comment=multiple to post those comments only if there are multiple CRs in the stack.


**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `-c`, `--fill`: Fill in the change title and body from the commit messages
* `--[no-]draft`: Whether to mark change requests as drafts
* `--[no-]publish` ([:material-wrench:{ .middle title="spice.submit.publish" }](/cli/config.md#spicesubmitpublish)): Whether to create CRs for pushed branches. Defaults to true.
* `-w`, `--[no-]web` ([:material-wrench:{ .middle title="spice.submit.web" }](/cli/config.md#spicesubmitweb)): Open submitted changes in a web browser
* `--nav-comment=true` ([:material-wrench:{ .middle title="spice.submit.navigationComment" }](/cli/config.md#spicesubmitnavigationcomment)): Whether to add a navigation comment to the change request. Must be one of: true, false, multiple.
* `--force`: Force push, bypassing safety checks
* `-u`, `--update-only`: Only update existing change requests, do not create new ones
* `--branch=NAME`: Branch to start at

**Configuration**: [spice.submit.navigationComment](/cli/config.md#spicesubmitnavigationcomment), [spice.submit.publish](/cli/config.md#spicesubmitpublish), [spice.submit.web](/cli/config.md#spicesubmitweb)

### gs upstack restack

```
gs upstack (us) restack (r) [flags]
```

Restack a branch and its upstack

The current branch and all branches above it
are rebased on top of their respective bases,
ensuring a linear history.
Use --branch to start at a different branch.
Use --skip-start to skip the starting branch,
but still rebase all branches above it.

The target branch defaults to the current branch.
If run from the trunk branch,
all managed branches will be restacked.

**Flags**

* `--branch=NAME`: Branch to restack the upstack of
* `--skip-start`: Do not restack the starting branch

### gs upstack onto

```
gs upstack (us) onto (o) [<onto>] [flags]
```

Move a branch onto another branch

The current branch and its upstack will move onto the new base.

For example, given the following stack with B checked out,
'gs upstack onto main' will have the following effect:

	       gs upstack onto main

	    ┌── C                 ┌── C
	  ┌─┴ B ◀               ┌─┴ B ◀
	┌─┴ A                   ├── A
	trunk                   trunk

Use 'gs branch onto' to leave the branch's upstack alone.

Use --branch to move a different branch than the current one.

A prompt will allow selecting the new base.
Use the spice.branchPrompt.sort configuration option
to specify the sort order of branches in the prompt.
Commonly used field names include "refname", "commiterdate", etc.
By default, branches are sorted by name.
Provide the new base name as an argument to skip the prompt.

**Arguments**

* `onto`: Destination branch

**Flags**

* `--branch=NAME`: Branch to start at

**Configuration**: [spice.branchPrompt.sort](/cli/config.md#spicebranchpromptsort)

### gs downstack submit

```
gs downstack (ds) submit (s) [flags]
```

Submit a branch and those below it

Change Requests are created or updated
for the current branch and all branches below it until trunk.
Use --branch to start at a different branch.

Use --dry-run to print what would be submitted without submitting it.
For new Change Requests, a prompt will allow filling metadata.
Use --fill to populate title and body from the commit messages,
and --[no-]draft to set the draft status.
Omitting the draft flag will leave the status unchanged of open CRs.

Use --no-publish to push branches without creating CRs.
This has no effect if a branch already has an open CR.
Use --update-only to only update branches with existing CRs,
and skip those that would create new CRs.

Use --nav-comment=false to disable navigation comments in CRs,
or --nav-comment=multiple to post those comments only if there are multiple CRs in the stack.


**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `-c`, `--fill`: Fill in the change title and body from the commit messages
* `--[no-]draft`: Whether to mark change requests as drafts
* `--[no-]publish` ([:material-wrench:{ .middle title="spice.submit.publish" }](/cli/config.md#spicesubmitpublish)): Whether to create CRs for pushed branches. Defaults to true.
* `-w`, `--[no-]web` ([:material-wrench:{ .middle title="spice.submit.web" }](/cli/config.md#spicesubmitweb)): Open submitted changes in a web browser
* `--nav-comment=true` ([:material-wrench:{ .middle title="spice.submit.navigationComment" }](/cli/config.md#spicesubmitnavigationcomment)): Whether to add a navigation comment to the change request. Must be one of: true, false, multiple.
* `--force`: Force push, bypassing safety checks
* `-u`, `--update-only`: Only update existing change requests, do not create new ones
* `--branch=NAME`: Branch to start at

**Configuration**: [spice.submit.navigationComment](/cli/config.md#spicesubmitnavigationcomment), [spice.submit.publish](/cli/config.md#spicesubmitpublish), [spice.submit.web](/cli/config.md#spicesubmitweb)

### gs downstack edit

```
gs downstack (ds) edit (e) [flags]
```

Edit the order of branches below a branch

An editor opens with a list of branches in-order,
starting from the current branch until trunk.
The current branch is at the top of the list.
Use --branch to start at a different branch.

Modifications to the list will be reflected in the stack
when the editor is closed, and the topmost branch will be checked out.
If the file is cleared, no changes will be made.
Branches that are deleted from the list will be ignored.
Branches that are upstack of the current branch will not be modified.

**Flags**

* `--editor=STRING`: Editor to use for editing the downstack. Defaults to Git's default editor.
* `--branch=NAME`: Branch to edit from. Defaults to current branch.

## Branch

### gs branch track

```
gs branch (b) track (tr) [<branch>] [flags]
```

Track a branch

A branch must be tracked to be able to run gs operations on it.
Use 'gs branch create' to automatically track new branches.

The base is guessed by comparing against other tracked branches.
Use --base to specify a base explicitly.

**Arguments**

* `branch`: Name of the branch to track

**Flags**

* `-b`, `--base=BRANCH`: Base branch this merges into

### gs branch untrack

```
gs branch (b) untrack (untr) [<branch>]
```

Forget a tracked branch

The current branch is deleted from git-spice's data store
but not deleted from the repository.
Branches upstack from it are not affected,
and will use the next branch downstack as their new base.

Provide a branch name as an argument to target
a different branch.

**Arguments**

* `branch`: Name of the branch to untrack. Defaults to current.

### gs branch checkout

```
gs branch (b) checkout (co) [<branch>] [flags]
```

Switch to a branch

A prompt will allow selecting between tracked branches.
Provide a branch name as an argument to skip the prompt.

Use -u/--untracked to show untracked branches in the prompt.

Use the spice.branchPrompt.sort configuration option
to specify the sort order of branches in the prompt.
Commonly used field names include "refname", "commiterdate", etc.
By default, branches are sorted by name.

**Arguments**

* `branch`: Name of the branch to checkout

**Flags**

* `-n`, `--dry-run`: Print the target branch without checking it out
* `--detach`: Detach HEAD after checking out
* `-u`, `--untracked` ([:material-wrench:{ .middle title="spice.branchCheckout.showUntracked" }](/cli/config.md#spicebranchcheckoutshowuntracked)): Show untracked branches if one isn't supplied

**Configuration**: [spice.branchCheckout.showUntracked](/cli/config.md#spicebranchcheckoutshowuntracked), [spice.branchPrompt.sort](/cli/config.md#spicebranchpromptsort)

### gs branch create

```
gs branch (b) create (c) [<name>] [flags]
```

Create a new branch

Staged changes will be committed to the new branch.
If there are no staged changes, an empty commit will be created.
Use -a/--all to automatically stage modified and deleted files,
just like 'git commit -a'.
Use --no-commit to create the branch without committing.

If a branch name is not provided,
it will be generated from the commit message.

The new branch will use the current branch as its base.
Use --target to specify a different base branch.

--insert will move the branches upstack from the target branch
on top of the new branch.
--below will create the new branch below the target branch.

For example, given the following stack, with A checked out:

	    ┌── C
	  ┌─┴ B
	┌─┴ A ◀
	trunk

'gs branch create X' will have the following effects
with different flags:

	         gs branch create X

	 default  │   --insert   │  --below
	──────────┼──────────────┼──────────
	  ┌── X   │        ┌── C │       ┌── C
	  │ ┌── C │      ┌─┴ B   │     ┌─┴ B
	  ├─┴ B   │    ┌─┴ X     │   ┌─┴ A
	┌─┴ A     │  ┌─┴ A       │ ┌─┴ X
	trunk     │  trunk       │ trunk

In all cases above, use of -t/--target flag will change the
target (A) to the specified branch:

	     gs branch create X --target B

	 default  │   --insert   │  --below
	──────────┼──────────────┼────────────
	    ┌── X │        ┌── C │       ┌── C
	    ├── C │      ┌─┴ X   │     ┌─┴ B
	  ┌─┴ B   │    ┌─┴ B     │   ┌─┴ X
	┌─┴ A     │  ┌─┴ A       │ ┌─┴ A
	trunk     │  trunk       │ trunk

**Arguments**

* `name`: Name of the new branch

**Flags**

* `--insert`: Restack the upstack of the target branch onto the new branch
* `--below`: Place the branch below the target branch and restack its upstack
* `-t`, `--target=BRANCH`: Branch to create the new branch above/below
* `-a`, `--all`: Automatically stage modified and deleted files
* `-m`, `--message=MSG`: Commit message
* `--no-verify`: Bypass pre-commit and commit-msg hooks.
* `--[no-]commit` ([:material-wrench:{ .middle title="spice.branchCreate.commit" }](/cli/config.md#spicebranchcreatecommit)): Commit staged changes to the new branch, or create an empty commit

**Configuration**: [spice.branchCreate.commit](/cli/config.md#spicebranchcreatecommit)

### gs branch delete

```
gs branch (b) delete (d,rm) [<branches> ...] [flags]
```

Delete branches

The deleted branches and their commits are removed from the stack.
Branches above the deleted branches are rebased onto
the next branches available downstack.

A prompt will allow selecting the target branch if none are provided.
Use the spice.branchPrompt.sort configuration option
to specify the sort order of branches in the prompt.
Commonly used field names include "refname", "commiterdate", etc.
By default, branches are sorted by name.

**Arguments**

* `branches`: Names of the branches to delete

**Flags**

* `--force`: Force deletion of the branch

**Configuration**: [spice.branchPrompt.sort](/cli/config.md#spicebranchpromptsort)

### gs branch fold

```
gs branch (b) fold (fo) [flags]
```

Merge a branch into its base

Commits from the current branch will be merged into its base
and the current branch will be deleted.
Branches above the folded branch will point
to the next branch downstack.
Use the --branch flag to target a different branch.

**Flags**

* `--branch=NAME`: Name of the branch

### gs branch split

```
gs branch (b) split (sp) [flags]
```

Split a branch on commits

Splits the current branch into two or more branches at specific
commits, inserting the new branches into the stack
at the positions of the commits.
Use the --branch flag to specify a different branch to split.

By default, the command will prompt for commits to introduce
splits at.
Supply the --at flag one or more times to split a branch
without a prompt.

	--at COMMIT:NAME

Where COMMIT resolves to a commit per gitrevisions(7),
and NAME is the name of the new branch.
For example:

	# split at a specific commit
	gs branch split --at 1234567:newbranch

	# split at the previous commit
	gs branch split --at HEAD^:newbranch

**Flags**

* `--at=COMMIT:NAME,...`: Commits to split the branch at.
* `--branch=NAME`: Branch to split commits of.

### gs branch squash

```
gs branch (b) squash (sq) [flags]
```

<span class="mdx-badge"><span class="mdx-badge__icon">:material-tag:{ title="Released in version" }</span><span class="mdx-badge__text">[v0.11.0](/changelog.md#v0.11.0)</span>

Squash a branch into one commit

Squash all commits in the current branch into a single commit
and restack upstack branches.

An editor will open to edit the commit message of the squashed commit.
Use the -m/--message flag to specify a commit message without editing.

**Flags**

* `--no-verify`: Bypass pre-commit and commit-msg hooks.
* `-m`, `--message=MSG`: Use the given message as the commit message.

### gs branch edit

```
gs branch (b) edit (e)
```

Edit the commits in a branch

Starts an interactive rebase with only the commits
in this branch.
Following the rebase, branches upstack from this branch
will be restacked.

### gs branch rename

```
gs branch (b) rename (rn,mv) [<old-name> [<new-name>]]
```

Rename a branch

The following modes are supported:

	# Rename <old> to <new>
	gs branch rename <old> <new>

	# Rename current branch to <new>
	gs branch rename <new>

	# Rename current branch interactively
	gs branch rename

For branches renamed with 'git branch -m',
use 'gs branch track' and 'gs branch untrack'
to update the branch tracking.

**Arguments**

* `old-name`: Old name of the branch
* `new-name`: New name of the branch

### gs branch restack

```
gs branch (b) restack (r) [flags]
```

Restack a branch

The current branch will be rebased onto its base,
ensuring a linear history.
Use --branch to target a different branch.

**Flags**

* `--branch=NAME`: Branch to restack

### gs branch onto

```
gs branch (b) onto (on) [<onto>] [flags]
```

Move a branch onto another branch

The commits of the current branch are transplanted onto another
branch.
Branches upstack are moved to point to its original base.

For example, given the following stack with B checked out,
running 'gs branch onto main' will move B onto main
and leave C on top of A.

	       gs branch onto main

	    ┌── C               ┌── B ◀
	  ┌─┴ B ◀               │ ┌── C
	┌─┴ A                   ├─┴ A
	trunk                   trunk

Use --branch to move a different branch than the current one.

A prompt will allow selecting the new base.
Use the spice.branchPrompt.sort configuration option
to specify the sort order of branches in the prompt.
Commonly used field names include "refname", "commiterdate", etc.
By default, branches are sorted by name.
Provide the new base name as an argument to skip the prompt.

**Arguments**

* `onto`: Destination branch

**Flags**

* `--branch=NAME`: Branch to move

**Configuration**: [spice.branchPrompt.sort](/cli/config.md#spicebranchpromptsort)

### gs branch submit

```
gs branch (b) submit (s) [flags]
```

Submit a branch

A Change Request is created for the current branch,
or updated if it already exists.
Use the --branch flag to target a different branch.

For new Change Requests, a prompt will allow filling metadata.
Use the --title and --body flags to skip the prompt,
or the --fill flag to use the commit message to fill them in.
The --draft flag marks the change request as a draft.
For updating Change Requests,
use --draft/--no-draft to change its draft status.
Without the flag, the draft status is not changed.

Use --no-publish to push branches without creating CRs.
This has no effect if a branch already has an open CR.
Use --update-only to only update branches with existing CRs,
and skip those that would create new CRs.

Use --nav-comment=false to disable navigation comments in CRs,
or --nav-comment=multiple to post those comments only if there are multiple CRs in the stack.

**Flags**

* `-n`, `--dry-run`: Don't actually submit the stack
* `-c`, `--fill`: Fill in the change title and body from the commit messages
* `--[no-]draft`: Whether to mark change requests as drafts
* `--[no-]publish` ([:material-wrench:{ .middle title="spice.submit.publish" }](/cli/config.md#spicesubmitpublish)): Whether to create CRs for pushed branches. Defaults to true.
* `-w`, `--[no-]web` ([:material-wrench:{ .middle title="spice.submit.web" }](/cli/config.md#spicesubmitweb)): Open submitted changes in a web browser
* `--nav-comment=true` ([:material-wrench:{ .middle title="spice.submit.navigationComment" }](/cli/config.md#spicesubmitnavigationcomment)): Whether to add a navigation comment to the change request. Must be one of: true, false, multiple.
* `--force`: Force push, bypassing safety checks
* `-u`, `--update-only`: Only update existing change requests, do not create new ones
* `--title=TITLE`: Title of the change request
* `--body=BODY`: Body of the change request
* `--branch=NAME`: Branch to submit

**Configuration**: [spice.submit.listTemplatesTimeout](/cli/config.md#spicesubmitlisttemplatestimeout), [spice.submit.navigationComment](/cli/config.md#spicesubmitnavigationcomment), [spice.submit.publish](/cli/config.md#spicesubmitpublish), [spice.submit.web](/cli/config.md#spicesubmitweb)

## Commit

### gs commit create

```
gs commit (c) create (c) [flags]
```

Create a new commit

Staged changes are committed to the current branch.
Branches upstack are restacked if necessary.
Use this as a shortcut for 'git commit'
followed by 'gs upstack restack'.

**Flags**

* `-a`, `--all`: Stage all changes before committing.
* `--fixup=STRING`: Create a fixup commit.
* `-m`, `--message=STRING`: Use the given message as the commit message.
* `--no-verify`: Bypass pre-commit and commit-msg hooks.

### gs commit amend

```
gs commit (c) amend (a) [flags]
```

Amend the current commit

Staged changes are amended into the topmost commit.
Branches upstack are restacked if necessary.
Use this as a shortcut for 'git commit --amend'
followed by 'gs upstack restack'.

**Flags**

* `-a`, `--all`: Stage all changes before committing.
* `-m`, `--message=MSG`: Use the given message as the commit message.
* `--no-edit`: Don't edit the commit message
* `--no-verify`: Bypass pre-commit and commit-msg hooks.

### gs commit split

```
gs commit (c) split (sp) [flags]
```

Split the current commit

Interactively select hunks from the current commit
to split into new commits below it.
Branches upstack are restacked as needed.

**Flags**

* `-m`, `--message=MSG`: Use the given message as the commit message.
* `--no-verify`: Bypass pre-commit and commit-msg hooks.

## Rebase

### gs rebase continue

```
gs rebase (rb) continue (c) [flags]
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

Use the --no-edit flag to continue without opening an editor.
Make --no-edit the default by setting 'spice.rebaseContinue.edit' to false
and use --edit to override it.

**Flags**

* `--[no-]edit` ([:material-wrench:{ .middle title="spice.rebaseContinue.edit" }](/cli/config.md#spicerebasecontinueedit)): Whehter to open an editor to edit the commit message.

**Configuration**: [spice.rebaseContinue.edit](/cli/config.md#spicerebasecontinueedit)

### gs rebase abort

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

## Navigation

### gs up

```
gs up (u) [<n>] [flags]
```

Move up one branch

Checks out the branch above the current one.
If there are multiple branches with the current branch as base,
a prompt will allow picking between them.
Use the -n flag to print the branch without checking it out.

**Arguments**

* `n`: Number of branches to move up.

**Flags**

* `-n`, `--dry-run`: Print the target branch without checking it out
* `--detach`: Detach HEAD after checking out

### gs down

```
gs down (d) [<n>] [flags]
```

Move down one branch

Checks out the branch below the current branch.
If the current branch is at the bottom of the stack,
checks out the trunk branch.
Use the -n flag to print the branch without checking it out.

**Arguments**

* `n`: Number of branches to move up.

**Flags**

* `-n`, `--dry-run`: Print the target branch without checking it out
* `--detach`: Detach HEAD after checking out

### gs top

```
gs top (U) [flags]
```

Move to the top of the stack

Checks out the top-most branch in the current branch's stack.
If there are multiple possible top-most branches,
a prompt will ask you to pick one.
Use the -n flag to print the branch without checking it out.

**Flags**

* `-n`, `--dry-run`: Print the target branch without checking it out
* `--detach`: Detach HEAD after checking out

### gs bottom

```
gs bottom (D) [flags]
```

Move to the bottom of the stack

Checks out the bottom-most branch in the current branch's stack.
Use the -n flag to print the branch without checking it out.

**Flags**

* `-n`, `--dry-run`: Print the target branch without checking it out
* `--detach`: Detach HEAD after checking out

### gs trunk

```
gs trunk [flags]
```

Move to the trunk branch

**Flags**

* `-n`, `--dry-run`: Print the target branch without checking it out
* `--detach`: Detach HEAD after checking out

## gs version

```
gs version [flags]
```

Print version information and quit

**Flags**

* `--short`: Print only the version number.

