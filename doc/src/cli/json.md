---
title: JSON
icon: material/code-braces-box
description: >-
  Get output from git-spice in JSON format.
---

# JSON output

Some commands in git-spice support JSON output.
Unlike the default human-readable output (which is written to standard error),
JSON output is written to standard output
so that it can be piped to other programs.

```freeze language="terminal" float="right"
{green}${reset} gs ls {red}--json{reset} | {mag}jq{reset} .name
```

For commands that support it,
enable JSON output with the `--json` flag.

## Commands that support JSON output

This section lists the commands that support JSON output,
and describes the structure of their JSON output.

### $$gs log long$$, $$gs log short$$

<!-- gs:version unreleased -->

Writes a stream of JSON outputs to standard output,
one per line, each representing a tracked branch.
These objects take the following form:

```typescript
{
  // Name of the branch.
  name: string,

  // Whether this branch is the current branch.
  // May be omitted if false.
  current?: boolean,

  // Branch directly below this one in the stack.
  // 'gs down' will check out this branch.
  // Omitted if this is the trunk branch.
  down?: {
    // Name of the downstack branch.
    name: string,

    // Whether the downstack branch is in-sync with this one.
    // If false, the branch needs to be restacked.
    // May be omitted if false.
    needsRestack?: boolean,
  },

  // Zero or more branches directly above this one in the stack.
  // 'gs up' will check out one of these branches.
  // Omitted if there are no branches above this one.
  ups?: [
    {
      name: string, // name of the upstack branch
    }
  ]

  // Commits that are part of this branch.
  // Does not include commits that are part of downstack branches.
  // Preset only if 'gs log long' is used.
  // May be omitted if there are no commits in this branch.
  commits?: [
    {
      sha: string,     // full hash of the Git commit
      subject: string, // first line of the commit message
    },
  ],

  // Information about the Change Request for this branch.
  // This is present if the branch was submitted and published
  // (e.g. with 'gs branch submit').
  change?: {
    // Human-readable identifier for the Change Request.
    // This is the PR number for GitHub (e.g. "#123"),
    // and the MR IID for GitLab (e.g. "!123").
    id: string,

    // URL at which the Change Request can be viewed.
    url: string,

    // Current status of the Change Request.
    // Present only if '--status=true' (or 'spice.log.statusFormat=true').
    // May be omitted if the remote forge is unsupported,
    // authentication is missing, or the status could not be determined.
    status?: "open" | "closed" | "merged",
  },

  // Push status of the branch.
  // This is present if the branch was submitted, even if not published
  // (e.g. with 'gs branch submit --no-publish').
  push?: {
    ahead: int, // number of commits ahead of the remote branch
    behind: int,// number of commits behind the remote branch

    // Whether the branch needs to be pushed to the remote.
    // Always false if ahead and behind are both zero.
    // May be omitted if false.
    needsPush?: boolean,
  },
}
```
