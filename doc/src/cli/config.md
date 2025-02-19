---
title: Configuration
icon: material/wrench
description: >-
  Customizing the behavior of the git-spice CLI.
---

# CLI configuration

<!-- gs:version v0.4.0 -->

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

### spice.branchCheckout.showUntracked

<!-- gs:version v0.5.0 -->

When running $$gs branch checkout$$ without any arguments,
git-spice presents a prompt to select a branch to checkout.
This option controls whether untracked branches are shown in the prompt.

**Accepted values:**

- `true`
- `false` (default)

### spice.branchCheckout.sort

<!-- gs:version unreleased -->

When running $$gs branch checkout$$ without any arguments,
git-spice presents a prompt to select a branch to checkout.
This option controls whether branches are, by default,
sorted by a given git field. It is git-spice's equivalent of
[git's branch.sort](https://git-scm.com/docs/git-config#Documentation/git-config.txt-branchsort) parameter.

### spice.branchCreate.commit

<!-- gs:version v0.5.0 -->

Whether $$gs branch create$$ should commit staged changes to the new branch.
Set this to `false` to default to creating new branches without committing,
and use the `--commit` flag to commit changes when needed.

- `true` (default)
- `false`

### spice.forge.github.apiUrl

URL at which the GitHub API is available.
Defaults to `$GITHUB_API_URL` if set,
or computed from the GitHub URL if not set.

See also: [GitHub Enterprise](../setup/auth.md#github-enterprise).

### spice.forge.github.url

URL of the GitHub instance used for GitHub requests.
Defaults to `$GITHUB_URL` if set, or `https://github.com` otherwise.

See also: [GitHub Enterprise](../setup/auth.md#github-enterprise).

### spice.forge.gitlab.url

<!-- gs:version v0.9.0 -->

URL of the GitLab instance used for GitLab requests.
Defaults to `$GITLAB_URL` if set, or `https://gitlab.com` otherwise.

See also [GitLab Self-Hosted](../setup/auth.md#gitlab-self-hosted).

### spice.forge.gitlab.oauth.clientID

<!-- gs:version v0.9.0 -->

Client ID for OAuth authentication with GitLab.

Defaults to git-spice's built-in Client ID (valid only for https://gitlab.com)
or `$GITLAB_OAUTH_CLIENT_ID` if set.

For Self-Hosted GitLab instances, you must set this value to a custom Client ID.

See also [GitLab Self-Hosted](../setup/auth.md#gitlab-self-hosted).

### spice.log.all

Whether $$gs log short$$ and $$gs log long$$ should show all stacks by default,
instead of showing just the current stack.

**Accepted values:**

- `true`
- `false` (default)

### spice.rebaseContinue.edit

<!-- gs:version v0.10.0 -->

Whether $$gs rebase continue$$ should open an editor to modify the commit message
when continuing after resolving a rebase conflict.

If set to false, you can opt in to opening the editor with the `--edit` flag.

**Accepted values:**

- `true` (default)
- `false`

### spice.submit.listTemplatesTimeout

<!-- gs:version v0.8.0 -->

Maximum duration that $$gs branch submit$$ will wait
to receive a list of available CR templates from the forge.
If the timeout is reached, the command will proceed without a template.

Value must be a duration string such as `5s`, `1m`, `1h`, etc.
Defaults to `1s`.

Bump this value if you see warnings like any of the following:

```
WRN Failed to cache templates err="cache templates: write object: hash-object: signal: killed"
WRN Could not list change templates error="list templates: Post \"https://api.github.com/graphql\": context deadline exceeded"
```

Set to `0` to disable the timeout completely.

### spice.submit.navigationComment

Specifies whether CR submission commands ($$gs branch submit$$ and friends)
should post or update a navigation comment to the CR.

**Accepted values:**

- `true` (default): always post or update navigation comments
- `false`: don't post or update navigation comments
- `multiple`:
  post or update navigation comments only for stacks with at least two CRs

### spice.submit.publish

<!-- gs:version v0.5.0 -->

Whether submission commands ($$gs branch submit$$ and friends)
should publish a CR to the forge.

If this is set to false, submit commands will push branches,
but not create CRs.
In that case, the `--publish` flag will opt-in to creating CRs
on a case-by-case basis.

**Accepted values:**

- `true` (default)
- `false`

### spice.submit.web

<!-- gs:version v0.8.0 -->

Whether submission commands ($$gs branch submit$$ and friends)
should open a web browser with submitted CRs.

**Accepted values:**

- `true`
- `false` (default)
