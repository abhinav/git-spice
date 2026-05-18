---
title: AI-powered messages
icon: material/robot
description: >-
  Use AI tools to generate and update
  commit messages and CR descriptions.
---

# AI-powered messages

<!-- gs:version unreleased -->

git-spice can invoke an external tool
to generate or update commit messages and CR descriptions.

This is done by configuring a single shell command
via `git config` under the `spice.messageGenerator` key.
When `--fill` is passed to a supported command,
git-spice runs the configured script
and uses its output as the message.

The script determines what to generate
based on environment variables provided by git-spice.

## Configuration

Set the message generator with:

```bash
git config --global spice.messageGenerator '<your script>'
```

See [Configuration](../cli/config.md) for full details.

## Supported commands

| Command | `GS_MESSAGE_KIND` | `GS_MESSAGE_UPDATE` |
|---------|--------------------|---------------------|
| `gs commit create --fill` | `commit` | `false` |
| `gs commit amend --fill` | `commit` | `true` |
| `gs branch create --fill` | `commit` | `false` |
| `gs branch submit --fill` (new CR) | `branch` | `false` |
| `gs branch submit --fill` (existing CR) | `branch` | `true` |
| `gs branch squash --fill` | `commit` | `true` |

## How it works

Scripts run in the repository root
and receive context via environment variables.

| Variable | Available in | Description |
|----------|-------------|-------------|
| `GS_MESSAGE_KIND` | All scripts | `commit` or `branch` |
| `GS_MESSAGE_UPDATE` | All scripts | `true` if updating, `false` if new |
| `GS_BRANCH` | All scripts | Current or submitting branch |
| `GS_BASE` | Branch scripts, branch create | Base branch name |
| `GS_MESSAGE` | Commit updater, squash | Existing commit message(s) |
| `GS_TITLE` | Branch updater | Existing CR title |
| `GS_BODY` | Branch updater | Existing CR body |

The invoking process's argument vector is also forwarded
to the script as positional parameters (`$@` in shell scripts).
This allows scripts to inspect the full gs command
if needed.

For branch scripts (generator and updater),
the output format is:

- **First line**: CR title
- **Blank line separator**
- **Remaining lines**: CR body

For commit scripts,
the entire output is used as the commit message.

If a script fails or produces empty output,
git-spice falls back to the default behavior
(opening the editor or using commit messages).

## Examples

### Using Claude Code

Configure a single generator that handles all message types.
The prompt uses `<message>` delimiters so that only
the intended message is extracted from the output,
discarding any reasoning or explanation.

```bash
git config --global spice.messageGenerator '#!/bin/sh

# Compute the relevant diff based on context.
# For branch scripts, use $GS_BRANCH (not HEAD)
# so that stack submit diffs each branch correctly.
if [ "$GS_MESSAGE_KIND" = "branch" ]; then
  DIFF=$(git diff "$GS_BASE"..."$GS_BRANCH")
elif [ "$GS_MESSAGE_UPDATE" = "true" ]; then
  DIFF=$(git diff HEAD~1)
else
  DIFF=$(git diff --cached)
fi

claude --no-session-persistence -p "
Generate a git message based on the following diff.
GS_MESSAGE_KIND=$GS_MESSAGE_KIND
GS_MESSAGE_UPDATE=$GS_MESSAGE_UPDATE.

If kind is commit, write a concise commit message
following conventional commits format, which uses
a summary first, a blank line, and then a
description of the change.
If kind is branch, write a PR title (first line),
blank line, then PR body. The title must be plain
text without markdown formatting (no # prefixes).
Describe ONLY the specific changes shown in the
diff. Do not describe the broader feature or
changes from other branches in the stack.
If updating, improve the existing message:
$GS_MESSAGE $GS_TITLE $GS_BODY

Here is the diff:
$DIFF

Output your message between <message> and </message> tags.
Only the content between these tags will be used.
" --output-format text 2>/dev/null \
  | sed -n "/<message>/,/<\/message>/{/<message>/d;/<\/message>/d;p;}"
'
```

!!! tip

    `--no-session-persistence` prevents each invocation
    from cluttering your session log.
    For faster startup, add `--bare` and set `ANTHROPIC_API_KEY`
    in your environment (`--bare` skips keychain/OAuth reads).

### Using a simple shell script

```bash
git config --global spice.messageGenerator \
  'echo "feat: auto-generated message for $GS_MESSAGE_KIND"'
```

### Using a shebang script

If the configuration value starts with `#!`,
git-spice writes it to a temporary file
and executes it with the specified interpreter:

```bash
git config --global spice.messageGenerator '#!/usr/bin/env python3
import subprocess, os
kind = os.environ.get("GS_MESSAGE_KIND", "commit")
update = os.environ.get("GS_MESSAGE_UPDATE", "false")
diff = subprocess.check_output(
    ["git", "diff", "--cached", "--stat"],
    text=True,
)
if kind == "commit":
    print(f"Update {len(diff.splitlines())} files")
else:
    print(f"PR: Update {len(diff.splitlines())} files")
    print()
    print("Automated PR description.")
'
```
