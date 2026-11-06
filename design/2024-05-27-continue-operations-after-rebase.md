# Continuing operations with `gs rebase continue`

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
