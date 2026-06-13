---
title: AI-powered messages
icon: material/robot
description: >-
  Use AI tools to generate and update
  commit messages and CR descriptions.
---

# AI-powered messages

<!-- gs:version unreleased -->

git-spice can invoke an external script to generate or update commit
messages and CR descriptions. The script speaks the shared script
protocol — see **[Script integrations](scripts.md)** for the
JSON output schema, environment contract, and failure semantics.
This page only documents what is message-generation-specific.

## Configuration

Two git-config keys control the feature:

- $$spice.message.generator$$ — the generator script body. The value
  is the script itself (not a path); git-spice runs it via `sh -c`,
  or executes it directly if it starts with `#!`. Typically set at
  `--global` scope.
- $$spice.message.autoFill$$ — when `true`, the generator runs by
  default and `--no-fill` opts out per invocation. Defaults to
  `false`; `--fill` is the explicit opt-in.

Per-invocation overrides on every fill-aware command:

- `--fill` runs the generator for this invocation.
- `--no-fill` skips it, even when $$spice.message.autoFill$$ is on.

## Supported commands and operation values

`GS_OPERATION` (from the
[shared env contract](scripts.md#environment-contract)) is set per
caller:

| Command | `GS_OPERATION` | `GS_MESSAGE_UPDATE` |
|---------|----------------|---------------------|
| `gs commit create --fill` | `commit-create` | `false` |
| `gs commit amend --fill` | `commit-amend` | `true` |
| `gs branch create --fill` | `branch-create` | `false` |
| `gs branch submit --fill` (new CR) | `branch-submit` | `false` |
| `gs branch submit --fill` (existing CR) | `branch-submit` | `true` |
| `gs branch squash --fill` | `branch-squash` | `true` |

## Message-specific environment

In addition to the shared
[`GS_OPERATION` / `GS_BRANCH` / `GS_BASE`](scripts.md#environment-contract)
values, message-generation scripts also receive:

| Variable | Available in | Meaning |
|---|---|---|
| `GS_MESSAGE_UPDATE` | All scripts | `true` if updating an existing message |
| `GS_MESSAGE` | Commit amend, squash | Existing commit message |
| `GS_TITLE` | Branch submit (existing CR) | Existing CR title |
| `GS_BODY` | Branch submit (existing CR) | Existing CR body |

## Output

Scripts emit the [shared JSON
protocol](scripts.md#output-contract). For message generation the
relevant fields are:

- `title` (required) — the first-line commit subject or CR title.
- `body` (optional) — multi-line message body.
- `assumptions` (optional) — surfaced in the gs log.
- `questions` (optional) — drive an interactive Q&A loop; answers
  persist in `.spice/resolutions/message.json` and are reused on
  subsequent runs for the same branch.

## Examples

### Using Claude Code

Configure a single generator that handles all message types. The
prompt uses `<message>` delimiters so that only the intended JSON
payload is extracted from the output, discarding any reasoning or
explanation.

```bash
git config --global spice.message.generator '#!/bin/sh

# Compute the relevant diff based on context.
if [ "${GS_OPERATION%-*}" = "branch" ]; then
  DIFF=$(git diff "$GS_BASE"..."$GS_BRANCH")
elif [ "$GS_MESSAGE_UPDATE" = "true" ]; then
  DIFF=$(git diff HEAD~1)
else
  DIFF=$(git diff --cached)
fi

claude --no-session-persistence -p "
You are helping write a git message. GS_OPERATION=$GS_OPERATION,
GS_MESSAGE_UPDATE=$GS_MESSAGE_UPDATE.

Operations starting with commit- or branch-create expect a commit
message; operations starting with branch- (submit/squash) expect a
CR title and body. Describe ONLY the changes shown in the diff.
Do not describe the broader feature or changes from other branches
in the stack.

If updating, improve the existing message:
$GS_MESSAGE $GS_TITLE $GS_BODY

Here is the diff:
$DIFF

Output a JSON document between <message> and </message> tags
matching the shared script-integration protocol:

  <message>
  {\"title\":\"<one-line title>\",\"body\":\"<body, may be empty>\"}
  </message>
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
git config --global spice.message.generator \
  '#!/bin/sh
printf '\''{"title":"feat: auto-generated for %s"}'\'' "$GS_OPERATION"'
```
