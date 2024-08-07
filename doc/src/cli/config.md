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
{green}${reset} git config --global {red}<key>{reset} {mag}<value>{reset}

{gray}# Set an option for current repository{reset}
{green}${reset} git config --local {red}<key>{reset} {mag}<value>{reset}
```

??? question "What about `--system` and `--worktree`?"

    All configuration levels supported by `git config` are allowed,
    although `--system` and `--worktree` are less commonly used.
    Use `--worktree` to override repository-level settings
    for a specific [git-worktree](https://git-scm.com/docs/git-worktree).

## Available options

### spice.forge.github.apiUrl

URL at which the GitHub API is available.
Defaults to `$GITHUB_API_URL` if set, or `https://api.github.com` otherwise.

See also: [GitHub Enterprise](../setup/auth.md#github-enterprise).

### spice.forge.github.url

URL of the GitHub instance used for GitHub requests.
Defaults to `$GITHUB_URL` if set, or `https://github.com` otherwise.

See also: [GitHub Enterprise](../setup/auth.md#github-enterprise).

### spice.log.all

Whether $$gs log short$$ and $$gs log long$$ should show all stacks by default,
instead of showing just the current stack.

**Accepted values:**

- `true`
- `false` (default)
