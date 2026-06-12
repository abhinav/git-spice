---
title: Auto-resolving restack conflicts
icon: octicons/zap-16
description: >-
  Have a user-configured script resolve rebase conflicts during
  gs branch/upstack/downstack/stack/repo restack.
---

# Auto-resolving restack conflicts

<!-- gs:version unreleased -->

Restacking a branch onto its base is a `git rebase` under the hood,
and rebases can conflict. By default, git-spice surfaces those
conflicts to you (the worktree is left mid-rebase, you resolve, then
run $$gs rebase continue$$).

For local, exploratory stacks where the resolution is often
mechanical, you can plug in a **resolver script** that git-spice
invokes when a rebase step conflicts. If the script reports the
conflict as resolved, git-spice stages the files and tells
`git rebase` to continue — without leaving the worktree mid-rebase
and without prompting you.

The protocol the script speaks (JSON output schema, environment
contract, persistent resolution file, iteration cap, fall-through
rules) is the same across all of git-spice's script-driven features.
See **[Script integrations](scripts.md)** for the complete contract.
This page only documents what is restack-specific.

The feature is **off by default** and opt-in. Nothing changes unless
you set $$spice.restack.resolver$$ and (optionally)
$$spice.restack.autoResolve$$.

## Configuration

Two git-config keys control the feature:

- $$spice.restack.resolver$$ — the resolver script body. The value
  is the script itself (not a path); git-spice runs it via `sh -c`,
  or executes it directly if it starts with `#!`. Typically set at
  `--global` scope since the resolver is a personal preference.
- $$spice.restack.autoResolve$$ — when `true`, the resolver runs
  automatically on every restack command.

The shared $$spice.scriptResolve.maxIterations$$ key bounds the
Q&A loop. See [Script integrations](scripts.md#iteration-cap).

Per-invocation overrides are available on every restacking command
($$gs branch restack$$, $$gs upstack restack$$, $$gs downstack
restack$$, $$gs stack restack$$, $$gs repo restack$$):

- `--auto-resolve` enables auto-resolve for this invocation.
- `--no-auto-resolve` disables it, even when the config has it on.

## Restack-specific environment

In addition to the shared
[`GS_OPERATION` / `GS_BRANCH` / `GS_BASE`](scripts.md#environment-contract)
values, the resolver runs from the repository root inside an
in-progress `git rebase`. The conflicted files contain
`<<<<<<<` / `=======` / `>>>>>>>` markers; the script reads them, edits
the files in place, and emits a JSON response. git-spice then stages
the originally-conflicted paths and runs `git rebase --continue`.

`GS_OPERATION` is one of: `branch-restack`, `upstack-restack`,
`downstack-restack`, `stack-restack`, `repo-restack`. `GS_BRANCH` is
the branch whose commits are being replayed; `GS_BASE` is the branch
being replayed onto (also recorded in the resolution file's
`current_merge.theirs` and `current_merge.ours` fields, respectively).

## Resolution file

Restack's persistent Q&A lives at
`<repo-root>/.spice/resolutions/restack.json`. The schema is
described in
[Script integrations: persistent resolution files](scripts.md#persistent-resolution-files).
Each entry is keyed by the `(ours, theirs)` branch pair, so an answer
recorded on one restack is reused on subsequent restacks of the same
pair.

## Example: Claude Code resolver

The resolver is configured as a user-level preference — the config
value is the script body itself, run by git-spice via `sh -c`.
Use `--global` so the script applies to every repo you restack in.

Paste the block below into a terminal to install a resolver that
delegates to [Claude Code](https://docs.claude.com/en/docs/claude-code)
and turn auto-resolve on by default:

```bash
git config --global spice.restack.resolver "$(cat <<'GITCONFIG'
#!/bin/sh
claude --print --max-turns 30 --output-format text 2>/dev/null <<'PROMPT' \
  | sed -n '/<resolution>/,/<\/resolution>/{/<resolution>/d;/<\/resolution>/d;p;}'
You are resolving rebase conflicts during a git-spice restack.

CONTEXT
- 'git ls-files --unmerged' lists the conflicted paths.
- 'git log -p HEAD' shows the commit being rebased ("theirs"); the
  branch being rebased onto is the working state ("ours").
- GS_OPERATION, GS_BRANCH, and GS_BASE are set in the environment;
  see https://abhinav.github.io/git-spice/guide/scripts/ for the
  full env contract.
- .spice/resolutions/restack.json names the rebase in progress
  (current_merge: ours = GS_BASE, theirs = GS_BRANCH) and carries any
  prior Q&A you have recorded under resolutions. Honor that prior
  guidance when it applies.

WHAT TO DO
- Edit each conflicted file in place. Remove the <<<<<<<, =======,
  and >>>>>>> markers and produce a syntactically valid merged file.
- DEFAULT TO ADDITIVE MERGES. When each side adds an independent
  top-level declaration at the conflict boundary — a method on a
  struct, an exported package function, a struct field, an interface
  method, a config getter, a type definition, an import, a struct
  literal field — that is almost never a real conflict. Keep BOTH
  additions, in either order.
- Do NOT run 'git add' or 'git rebase --continue'. After you exit,
  git-spice stages every originally-conflicted path and continues
  the rebase.

OUTPUT — emit exactly one JSON document between <resolution> and
</resolution> tags. Only content between these tags is parsed.

  <resolution>
  {"assumptions": [...], "questions": [...], "unresolved_files": [...]}
  </resolution>

See https://abhinav.github.io/git-spice/guide/scripts/ for what each
field means and when to use it.
PROMPT
GITCONFIG
)"

git config --global spice.restack.autoResolve true
```

The `<resolution>...</resolution>` marker pattern (paired with the
`sed -n` extraction in the script) discards Claude's prose preface
without changing the JSON parser. If the model omits the markers
entirely, `sed` produces no output and git-spice halts the restack
with a parse error so the underlying problem can be fixed rather
than silently overwritten.

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

If you already run Claude Code with a permissive default — e.g.,
`permissions.defaultMode` set to `auto` or `bypassPermissions` —
you can omit the `allow` list entirely. Restacks are local
operations on tracked git history, so this is a reasonable
trade-off for unattended runs.
