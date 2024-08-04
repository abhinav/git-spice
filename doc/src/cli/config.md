---
title: Configuration
icon: material/wrench
description: >-
  Customizing the behavior of the git-spice CLI.
---

# CLI configuration

<!-- gs:version unreleased -->

The behavior of git-spice can be customized with `git config`.
Configuration options may be set at the user level with the `--global` flag,
or at the repository level with the `--local` flag.

```freeze language="terminal"
{gray}# Set an option for current user{reset}
{green}${reset} git config --global spice.{red}<key>{reset} {mag}<value>{reset}

{gray}# Set an option for current repository{reset}
{green}${reset} git config --local spice.{red}<key>{reset} {mag}<value>{reset}
```

??? question "What about `--system` and `--worktree`?"

    All configuration levels supported by `git config` are allowed,
    although `--system` and `--worktree` are less commonly used.
    Use `--worktree` to override repository-level settings
    for a specific [git-worktree](https://git-scm.com/docs/git-worktree).
