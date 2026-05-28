---
title: Integration branches
icon: octicons/git-merge-16
description: >-
  Build a single branch that combines multiple stack tips
  for testing or preview deploys.
---

# Integration branches

<!-- gs:version unreleased -->

An **integration branch** is a repo-scoped singleton that is rebuilt by
sequentially merging the tips of multiple tracked branches onto trunk.
It is intended for testing or preview builds that need to combine
several otherwise-independent stacks.

Integration branches are deliberately separate from regular stack
branches:

- They are never given a PR / change request.
- They are invisible to `gs branch` commands.
- They are regenerated, not edited: every rebuild force-overwrites the
  branch.

## Configuration

Configure the singleton with $$gs integration create$$:

```freeze language="terminal"
{green}${reset} gs integration create preview --tip feat-a --tip feat-b
```

The branch name (`preview` above) must not equal trunk. Tips must be
tracked branches; an untracked branch is rejected.

Add or remove tips later with $$gs integration tip add$$ and
$$gs integration tip remove$$.

## Rebuilding

Materialize or refresh the branch with $$gs integration rebuild$$:

```freeze language="terminal"
{green}${reset} gs integration rebuild
```

The worktree must be clean. Each tip is merged with `--no-ff`; `rerere`
is enabled for these merges so any recorded conflict resolutions are
replayed automatically. Rebuild can be invoked while the integration
branch itself is checked out; when finished, the worktree remains on
the integration branch.

If a tip conflicts, the merge is left in the worktree for you to
resolve. The conflicting paths are reported. To proceed:

1. Resolve the conflicts.
2. Stage the resolved files with `git add` and commit the merge with
   `git merge --continue`.
3. Re-run `gs integration rebuild` (or `gs intrb`). The rebuild
   resumes from the next tip after the one you just merged. The
   conflict resolution is also recorded by `rerere` so future
   rebuilds skip it automatically.

To bail out of the rebuild instead, run `git merge --abort`. The
pending-rebuild state remains; clear it by re-configuring the
integration with `gs integration delete` followed by `gs integration
create`.

### Generated-file conflicts

Some files in the repository are derived from source or test runs
(CLI reference, help fixtures, mocks, recorded ShamHub fixtures).
When two branches both touch them, the conflicts mix stochastic
noise (random IDs) with real structural changes (a new flag, a new
test case, a new mock method). Picking either side blindly silently
drops the structural change from the other branch.

The `.gitattributes` file declares a `regenerate` merge driver for
these paths. The driver re-runs the appropriate generator against
the merged source so the output reflects both branches' real
changes. To activate, run once after cloning:

```freeze language="terminal"
{green}${reset} mise run setup
```

After that, `gs intrb` (and any other `git merge` in the repo) will
auto-resolve generated-file conflicts by regenerating, not by
picking a side. Each generator runs at most once per merge even when
many files in its output set conflict.

## Switching to the integration branch

Use $$gs integration checkout$$ (shorthand: `gs intco`) to switch the
worktree to the integration branch:

```freeze language="terminal"
{green}${reset} gs intco
```

This fails if the branch has not yet been materialized; run
$$gs integration rebuild$$ first.

### Auto-rebuild

After any `gs branch restack`, `gs upstack restack`, `gs stack restack`,
or `gs repo restack`, the integration branch is automatically
regenerated when at least one of its tips has moved. The auto-rebuild
is silent when no tip has drifted.

## Submitting

Push the branch to the remote with $$gs integration submit$$:

```freeze language="terminal"
{green}${reset} gs integration submit
```

This pushes with `--force-with-lease` against the hash recorded at the
last successful push. **No change request is created.**

### Auto-submit

Once the integration branch has been pushed manually at least once,
subsequent `gs stack submit`, `gs upstack submit`, and
`gs downstack submit` invocations will keep the published branch in
sync by pushing the latest rebuild.

## Auto-resolving conflicts

<!-- gs:version unreleased -->

Even with rerere and the `regenerate` merge driver handling the
mechanical conflicts, rebuilds can still surface hand-resolution
conflicts when two tips touch the same file. For local-only integration
branches, you can configure a script that resolves these conflicts
automatically.

The protocol the script speaks (JSON output schema, environment
contract, persistent resolution file, iteration cap, fall-through
rules) is the same across all of git-spice's script-driven features.
See **[Script integrations](scripts.md)** for the complete contract.
This section only documents what is integration-specific.

### Configuration

Two git-config keys control the feature:

- $$spice.integration.resolver$$ — the resolver script body. The value
  is the script itself (not a path); git-spice runs it via `sh -c`,
  or executes it directly if it starts with `#!`. Typically set at
  `--global` scope since the resolver is a personal preference.
- $$spice.integration.autoResolve$$ — when `true`, the resolver runs
  automatically on every $$gs integration rebuild$$.

The shared $$spice.scriptResolve.maxIterations$$ key bounds the Q&A
loop. See [Script integrations](scripts.md#iteration-cap).

Per-invocation overrides are available on $$gs integration rebuild$$:

- `--auto-resolve` enables auto-resolve for this invocation.
- `--no-auto-resolve` disables it, even when the config has it on.

### Integration-specific environment

In addition to the shared
[`GS_OPERATION` / `GS_BRANCH` / `GS_BASE`](scripts.md#environment-contract)
values, the resolver runs from the repository root with an in-progress
`git merge`. `GS_OPERATION` is always `integration-rebuild`. `GS_BRANCH`
is the tip being merged in; `GS_BASE` is the integration branch (also
recorded in the resolution file's `current_merge.theirs` and
`current_merge.ours` fields, respectively).

### Resolution file

Integration's persistent Q&A lives at
`<repo-root>/.spice/resolutions/integration.json`. The schema is
described in
[Script integrations: persistent resolution files](scripts.md#persistent-resolution-files).
Each entry is keyed by the `(ours, theirs)` branch pair, so an answer
recorded on one rebuild is reused on subsequent rebuilds.

Entries are pruned automatically when their branches are untracked
($$gs branch untrack$$), deleted ($$gs branch delete$$), or removed
by $$gs repo sync$$ after the underlying CR merges.

### Example: Claude Code resolver

The resolver is configured as a user-level preference — the config
value is the script body itself, run by git-spice via `sh -c`,
the same shape as $$spice.messageGenerator$$. Use `--global` so the
script applies to every repo you run integration rebuilds in.

Paste the block below into a terminal to install a resolver that
delegates to [Claude Code](https://docs.claude.com/en/docs/claude-code)
and turn auto-resolve on by default:

```bash
git config --global spice.integration.resolver "$(cat <<'GITCONFIG'
#!/bin/sh
exec claude --print --max-turns 30 <<'PROMPT'
You are resolving merge conflicts on a throwaway integration branch.

CONTEXT
- 'git ls-files --unmerged' lists the conflicted paths.
- 'git log -p ours' and 'git log -p theirs' show the commits on each
  side; 'git diff ours theirs -- <path>' compares them on one file.
- GS_OPERATION, GS_BRANCH, and GS_BASE are set in the environment;
  see https://abhinav.github.io/git-spice/guide/scripts/ for the
  full env contract.
- .spice/resolutions/integration.json names the merge in progress
  (current_merge: ours = GS_BASE, theirs = GS_BRANCH) and carries
  any prior Q&A you have recorded under resolutions. Honor that
  prior guidance when it applies.

WHAT TO DO
- Edit each conflicted file in place. Remove the <<<<<<<, =======,
  and >>>>>>> markers and produce a syntactically valid merged file.
- Do NOT run 'git add' or 'git commit'. After you exit, git-spice
  stages every originally-conflicted path and commits the merge.

OUTPUT — emit exactly one JSON document on stdout, then exit:

  {"assumptions": [...], "questions": [...], "unresolved_files": [...]}

- All three keys are optional. Empty (or assumptions-only) means
  "everything resolved cleanly"; git-spice will stage and commit.
- "assumptions" — short notes on judgement calls you made. They are
  surfaced in the rebuild log so a human can spot-check them.
See https://abhinav.github.io/git-spice/guide/scripts/ for what each
field means and when to use it.
PROMPT
GITCONFIG
)"

git config --global spice.integration.autoResolve true
```

For Claude Code to run unattended, pre-approve the tools it needs in
`~/.claude/settings.json`. The schema is a `permissions` object with
an `allow` array of tool patterns
([reference](https://docs.claude.com/en/docs/claude-code/settings#permissions)):

```json
{
  "permissions": {
    "allow": [
      "Read",
      "Edit",
      "Bash(git log:*)",
      "Bash(git show:*)",
      "Bash(git diff:*)",
      "Bash(git ls-files:*)"
    ]
  }
}
```

Bare names like `Read` and `Edit` allow the tool on any path; scope
them with `Read(/repo/path/**)` if you'd rather only let the resolver
read inside the integration repo.

If you already run Claude Code with a permissive default — e.g.,
`permissions.defaultMode` set to `auto` (skip routine prompts) or
`bypassPermissions` (skip all prompts) — you can omit the `allow`
list entirely. The integration branch is throwaway, so this is a
reasonable trade-off for unattended rebuilds. See the upstream
[settings reference](https://docs.claude.com/en/docs/claude-code/settings#permissions)
for what each mode covers.

If a resolution turns out to be wrong, the integration branch is
throwaway: investigate the diff, update your prompt or hand-edit
`resolution_instructions` in `.spice/resolutions/integration.json`,
then run $$gs integration rebuild$$ again. If `rerere` has recorded
an incorrect resolution from an earlier run, `git rerere clear` wipes
the cache.

## Regenerating derived files

<!-- gs:version unreleased -->

Repositories often have files that are *derived* from source — CLI
documentation, mockgen output, recorded test fixtures — where two
integration tips' changes might conflict not because of meaningful
disagreement but because the same generator was re-run on each
branch with different stochastic IDs or with different inputs. A
classic merge driver can't handle these correctly because regenerating
during a merge would side-effect the worktree, and `git add` from
inside a merge driver fails (the index is locked).

git-spice handles this with a two-piece arrangement:

1. **A take-incoming git merge driver** registered for the path
   patterns you mark in `.gitattributes`. On conflict, it silently
   copies the incoming version into place AND appends the path to
   a log file whose location it reads from the
   `GS_INTEGRATION_REGEN_LOG` environment variable.
2. **A project-level regenerator script** at
   `.gs/integration-regenerate` (relative to repo root). After every
   successful $$gs integration rebuild$$, git-spice invokes this
   script with the deduplicated list of paths the merge driver
   handled, then folds the script's worktree output into the final
   merge commit.

### Setting it up in a new repo

Tag the derived paths in `.gitattributes`:

```gitattributes
doc/includes/cli-reference.md   merge=regenerate
testdata/help/*.txt             merge=regenerate
**/mocks_test.go                merge=regenerate
```

Register the merge driver. The driver itself is trivially small:

```sh
git config merge.regenerate.driver \
    "$(git rev-parse --git-path info)/merge-regenerate %O %A %B %P"
cat > "$(git rev-parse --git-path info)/merge-regenerate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cp -f "$3" "$2"
if [ -n "${GS_INTEGRATION_REGEN_LOG:-}" ]; then
    printf '%s\n' "$4" >> "$GS_INTEGRATION_REGEN_LOG"
fi
EOF
chmod +x "$(git rev-parse --git-path info)/merge-regenerate"
```

Add `.gs/integration-regenerate` to your repo (committed,
executable) with project-specific dispatch logic:

```sh
#!/bin/sh
# .gs/integration-regenerate
set -eu

# stdin is a deduplicated newline-separated list of paths whose
# conflicts the merge driver auto-resolved.
files=$(cat)

# Only run the slow mockgen pass if a mock was actually in the list.
if printf '%s\n' "$files" | grep -qE 'mocks?(_test)?\.go'; then
    go generate ./...
fi

# Only update help fixtures if a help file was in the list.
if printf '%s\n' "$files" | grep -q '^testdata/help/'; then
    go test -run TestHelp . -update
fi
```

The conditional dispatch keeps the steady-state cost low — a rebuild
with no derived-file conflicts pays one process spawn and exits.

### Contract

| Aspect | Value |
|--------|-------|
| Script path | `.gs/integration-regenerate` relative to repo root |
| Executable bit | required (`chmod +x`) |
| Input | newline-separated deduplicated paths on stdin |
| Working directory | repo root |
| Exit 0 | success; worktree changes folded into the last merge commit |
| Exit non-zero | warning logged; rebuild still considered successful |
| Absent | silently skipped (no-op) |

The path list is automatically allow-listed: only files whose
conflicts the `regenerate` merge driver actually handled appear in
the list. Conflicts on un-tagged files (regular source code) are
resolved through the usual channels and do *not* leak into the
regenerator.

## Removing the configuration

Use $$gs integration delete$$ to remove the configuration. The
underlying Git branch is not deleted.
