---
title: Script integrations
icon: octicons/code-square-16
description: >-
  Shared protocol for the user-configured scripts that git-spice invokes
  to generate commit messages and auto-resolve merge conflicts.
---

# Script integrations

<!-- gs:version unreleased -->

git-spice can hand off three operations to a user-configured script:

| Feature | Trigger | Configured by |
|---|---|---|
| Message generation | $$gs commit create --fill$$ and friends | $$spice.message.generator$$ |
| Restack auto-resolve | Rebase conflict during $$gs branch restack$$ et al. | $$spice.restack.resolver$$ |
| Integration auto-resolve | Merge conflict during $$gs integration rebuild$$ | $$spice.integration.resolver$$ |

All three speak the same protocol. This page documents the shared
contract so that a single script can be reused across features, and so
that wrapper tools (Claude Code, aider, custom shell scripts) only need
to be written once.

## How a script is invoked

git-spice resolves the configured value as a shell command. The
string may be either:

- A self-contained shebang script (`#!/usr/bin/env bash`,
  `#!/usr/bin/env python3`, etc.). git-spice writes it to a temporary
  file with mode 0700 and executes the temp file directly. The
  interpreter is whatever the shebang points to.
- A plain command, in which case `sh -c '<command>'` runs it.

In both cases:

- The script's working directory is the repository root.
- Parent environment is inherited, with `GIT_OPTIONAL_LOCKS=0`
  appended so that long-running scripts don't contend with git's index
  lock.
- The remaining arguments to the gs command are forwarded as
  positional parameters (`$1`, `$2`, ...). Most scripts ignore these.

## Environment contract

Every script receives at minimum:

| Variable | When | Meaning |
|---|---|---|
| `GS_OPERATION` | always | High-level gs operation (table below) |
| `GS_BRANCH` | when a branch is in scope | Branch being operated on |
| `GS_BASE` | when applicable | Base branch (for restack and message contexts) |

`GS_OPERATION` values:

| Value | Source |
|---|---|
| `commit-create` | $$gs commit create --fill$$ |
| `commit-amend` | $$gs commit amend --fill$$ |
| `branch-create` | $$gs branch create --fill$$ |
| `branch-submit` | $$gs branch submit --fill$$ (CR title/body) |
| `branch-squash` | $$gs branch squash --fill$$ |
| `branch-restack` | $$gs branch restack$$ auto-resolve |
| `upstack-restack` | $$gs upstack restack$$ auto-resolve |
| `downstack-restack` | $$gs downstack restack$$ auto-resolve |
| `stack-restack` | $$gs stack restack$$ auto-resolve |
| `repo-restack` | $$gs repo restack$$ auto-resolve |
| `integration-rebuild` | $$gs integration rebuild$$ auto-resolve |

Feature-specific extras layer on top of the shared minimum. For
message-generation operations:

| Variable | Meaning |
|---|---|
| `GS_MESSAGE_KIND` | `commit` or `branch` |
| `GS_MESSAGE_UPDATE` | `true` (amend/update) or `false` (new) |
| `GS_MESSAGE` | Existing commit message (commit amend, squash) |
| `GS_TITLE` | Existing CR title (branch update) |
| `GS_BODY` | Existing CR body (branch update) |

Auto-resolve operations expose state through the worktree itself
(`git status`, `git diff`, conflict markers) rather than via env vars.

## Output contract

Scripts emit a single JSON document on stdout. The schema is the union
of all field uses; each feature reads the subset that applies.

```json
{
  "title": "string",
  "body": "string",
  "assumptions": ["string", "..."],
  "questions": ["string", "..."],
  "unresolved_files": ["string", "..."]
}
```

| Field | Read by | Meaning |
|---|---|---|
| `title` | Message generation | First-line commit message / CR title (required for message gen) |
| `body` | Message generation | Body content (optional) |
| `assumptions` | All features | Notes the script wants surfaced in the gs log so the user sees what was assumed |
| `questions` | All features | Clarifying questions; trigger the interactive Q&A loop |
| `unresolved_files` | Auto-resolve | Files the script could not resolve; treated as "not done" and re-invoked |

Missing fields are valid (assumed empty / null). Extra fields are
ignored. Anything written to stderr is captured and logged at debug
level (or surfaced if the script exits non-zero).

### Assumptions

Each entry in `assumptions[]` is logged at info level prefixed with the
feature name. This is how the script tells the user "I picked X
because Y" without halting for confirmation.

### Questions

If `questions[]` is non-empty, git-spice prompts the user for each
question in order. Answers are appended to the persistent resolution
file (see below) and the script is re-invoked with the additional Q&A
context available via the resolution file. The loop continues until
the script returns an empty `questions[]` (a final answer), or
the iteration cap is reached.

### Termination

A run is considered terminal when **all** of these are true:

- The script exited zero.
- `questions[]` is empty.
- For auto-resolve operations, `unresolved_files[]` is empty.
- For message-generation operations, `title` is non-empty.

If termination conditions are not met but the iteration cap is
reached, git-spice logs a warning and falls back to the manual path
(open editor for messages; surface conflict for auto-resolve).

## Persistent resolution files

When a script returns `questions[]`, the prompts and the user's
answers are recorded in
`<repo-root>/.spice/resolutions/<feature>.json` (one file per feature:
`message.json`, `restack.json`, `integration.json`). The next run on
the same branch reads the file so a previously-answered question is
not re-asked.

The `.spice/` directory is created on first use. Whether to commit it
is a project decision — share for team-wide reuse, ignore for purely
local context.

Entries are pruned when the associated branch is removed via
$$gs branch delete$$ or $$gs branch untrack$$.

## Iteration cap

Each Q&A loop is bounded by $$spice.scriptResolve.maxIterations$$
(default 10). Set per repo or globally:

```freeze language="terminal"
{green}${reset} git config {red}spice.scriptResolve.maxIterations{reset} {mag}20{reset}
```

The cap is a safety backstop — a script that always returns
`questions[]` would otherwise loop forever.

## Worked examples

### Minimal commit-message generator

A shebang script that reads the staged diff and emits a one-line
title:

```bash
#!/usr/bin/env bash
set -euo pipefail

diff=$(git diff --cached)
title=$(echo "$diff" | head -1 | sed 's/^.*://' | head -c 60)

cat <<JSON
{
  "title": "$title",
  "assumptions": ["title derived from first diff line"]
}
JSON
```

### Auto-resolve via custom merge driver

A resolver that delegates regeneration to a script bundle and reports
each step as an assumption:

```bash
#!/usr/bin/env bash
set -euo pipefail

conflicts=$(git diff --name-only --diff-filter=U)
unresolved=()
assumptions=()

while read -r f; do
  if [[ -z "$f" ]]; then continue; fi
  if [[ -f "scripts/regen/$f.sh" ]]; then
    "scripts/regen/$f.sh"
    git add "$f"
    assumptions+=("regenerated $f")
  else
    unresolved+=("$f")
  fi
done <<< "$conflicts"

jq -n \
  --argjson assumptions "$(printf '%s\n' "${assumptions[@]}" | jq -R . | jq -s .)" \
  --argjson unresolved "$(printf '%s\n' "${unresolved[@]}" | jq -R . | jq -s .)" \
  '{assumptions: $assumptions, unresolved_files: $unresolved}'
```

### LLM-backed wrapper

Scripts that call out to an LLM follow the same protocol: marshal
context into a prompt, call the LLM, parse the response into the JSON
shape above. The LLM does not need to know it's behind gs — only the
wrapper script does.

## Error handling

A script can fail at three layers:

| Failure | gs behavior |
|---|---|
| Script not configured | $$gs commit create --fill$$ errors with a clear message pointing at $$spice.message.generator$$. Auto-resolve features silently skip if no resolver is configured. |
| Script exits non-zero | Logged at warn level. Message generation falls back to the editor; auto-resolve falls back to surfacing the conflict for manual resolution. The exit code and a truncated stderr appear in the log. |
| Script output is invalid JSON | Same as non-zero exit, with a "parse" stage marker in the log. |
| `questions[]` keeps growing past the cap | Warn logged; fall back to manual path. |

All failure paths are non-fatal — the script's misbehavior never
strands the user in a half-finished state.

## Configuration reference

The full list of script-related config keys:

| Key | Type | Default | Read by |
|---|---|---|---|
| `spice.message.generator` | string | "" | Message generation |
| `spice.message.autoFill` | bool | false | Message generation |
| `spice.restack.resolver` | string | "" | Restack auto-resolve |
| `spice.restack.autoResolve` | bool | false | Restack auto-resolve |
| `spice.integration.resolver` | string | "" | Integration auto-resolve |
| `spice.integration.autoResolve` | bool | false | Integration auto-resolve |
| `spice.scriptResolve.maxIterations` | int | 10 | All script-driven features |

See the [configuration reference](../cli/config.md) for the full
schema with link-anchored documentation.
