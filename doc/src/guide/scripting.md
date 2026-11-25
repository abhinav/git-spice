---
title: Scripting
icon: material/script-text-outline
description: Use git-spice in scripts and automate workflow steps.
---

# Scripting and automation

git-spice attempts to be compatible with scripts and automated workflows.

## Non-interactive usage

All commands support `--no-prompt` to disable interactive prompts.

With prompts disabled,
actions that previously prompted for a choice
will default to the safest option.

If git-spice encounters a choice it cannot make automatically,
it exits with an error.
For example, `gs up --no-prompt` fails if there are multiple branches
stacked directly above the current one.

### Submitting non-interactively

<!-- gs:version unreleased -->

To submit new change requests in non-interactive mode,
supply the title and body in advance.
Use `--draft` or `--no-draft` to set the draft status for all new CRs.

The title and body can be supplied with the `--message-file` option.
The flag is accepted in two forms:

- `--message-file PATH` for $$gs branch submit$$
- `--message-file BRANCH:PATH` for batch submission commands
  ($$gs stack submit$$, $$gs upstack submit$$, $$gs downstack submit$$)

`PATH` specifies a file containing the message.
The first line of the file is used as the title,
and the remaining text is used as the body (optional).

See [Submitting change requests](cr.md) for the rest of the submission workflow.

## Generating messages

<!-- gs:version unreleased -->

`--fill` supplies a message automatically when creating or amending a commit,
creating or squashing a branch, or submitting a new change request.
$$spice.message.fill$$ can be set to true to make this the default,
using `--no-fill` to opt out for one invocation.

```sh
gs commit create --fill

git config --global spice.message.fill true
gs commit amend --no-fill
```

Set $$spice.message.fill.command$$ to choose a shell command or script
that fills the message.

```sh
git config --global spice.message.fill.command '/path/to/generate-message'
```

Explicit messages override both `--fill` and the configured default.

For $$gs branch create$$, filling applies only when creating a commit.
Use `--commit` to enable commits if they are disabled by configuration.
With `--no-commit`, `--fill` has no effect.

### Command contract

The command runs from the worktree root
and is passed the path to the message file as its first argument,
followed by the hyphenated operation name,
and, for squash and submit, the base and head commit hashes.

```text
command MESSAGE_FILE OPERATION [BASE_SHA] [HEAD_SHA]
```

The command can run an editor, ask questions, or print progress.
It should write the final message to the file and exit with status 0.

A nonzero exit status aborts the operation.

`OPERATION` is the hyphenated name of the operation being performed.
For example, `commit-create`, `branch-squash`, or `stack-submit`.

| Operation | Initial file and Git context |
|---|---|
| `commit-create` | Git prepares the initial message and index. Use `git diff --cached` to inspect the proposed commit. |
| `commit-amend` | Git supplies the existing commit message. Use `git diff --cached` for the amendment and `git show HEAD` for the original commit. |
| `branch-create` | Git prepares the message and index after checking out the selected base. Use `git diff --cached` to compare the index to that base. |
| `branch-squash` | The message file starts empty. Use `git log "$3..$4"` to read the commits being squashed or `git diff "$3" "$4"` for their net changes. |
| `branch-submit`, `stack-submit`, `upstack-submit`, `downstack-submit` | The message file starts empty. Use `git diff "$3" "$4"` for the proposed change. For batch submission, the command runs once per new CR. |

For commit creation, amendment, and branch creation,
`git diff --cached` works the same way with or without `--all`.
The message file may already contain trailers and edits from
a `prepare-commit-msg` hook, if any.
Fill commands should preserve those if they should appear in the final message.

The first line of the message file is used as the title,
and the remaining text is used as the body (optional).
A title alone supplies an empty body.

### Examples

??? example "Edit a message with Vim"

    Configure this shell command to open the message file in Vim.
    Run it from an interactive terminal.

    ```sh
    git config spice.message.fill.command 'vim "$1"'
    gs commit create --fill
    ```

    This is demonstrative only; we do not recommend this command
    as it contains no information to write the commit message with.

??? example "Fill a message with a CLI assistant"

    This script uses `$harness` as a placeholder for an assistant CLI
    that can run Git commands and edit files.
    Adapt the script to your assistant's usage and capabilities.

    ```bash
    #!/usr/bin/env bash
    set -eu
    harness=/path/to/your-assistant-cli

    PROMPT=("Propose a commit-style message for operation $2.")
    case "$2" in
      commit-create|branch-create)
        PROMPT+=("Run 'git diff --cached' to see the changes being committed.")
        ;;
      commit-amend)
        PROMPT+=("Commit being amended: $(git rev-parse HEAD)")
        PROMPT+=("Run 'git show HEAD' to inspect the original commit.")
        PROMPT+=("Run 'git diff --cached' to see the changes being amended.")
        PROMPT+=("Describe the resulting commit, including the amendment.")
        ;;
      branch-squash)
        PROMPT+=("Commits being squashed:")
        PROMPT+=("$(git log --oneline "$3..$4")")
        PROMPT+=("Run 'git diff "$3" "$4"' to see the net changes.")
        ;;
      *-submit)
        PROMPT+=("Commits being submitted:")
        PROMPT+=("$(git log --oneline "$3..$4")")
        PROMPT+=("Run 'git diff "$3" "$4"' to see the proposed change.")
        ;;
    esac

    PROMPT+=("Read the message draft at $1 and edit it in place.")
    PROMPT+=("Preserve its trailers and hook-provided content.")

    IFS=$'\n'
    "$harness" "${PROMPT[*]}"
    ```
