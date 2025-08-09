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

### spice.branchCheckout.trackUntrackedPrompt

<!-- gs:version v0.14.0 -->

If $$gs branch checkout$$ is used to checkout a branch that is not tracked,
git-spice will present a prompt like the following to begin tracking it:

```freeze language="terminal"
{green}${reset} gs branch checkout {red}mybranch{reset}
{yellow}WRN{reset} mybranch: branch not tracked
{green}Do you want to track this branch now?{reset}: [{mag}Y{reset}/{mag}n{reset}]
```

This option allows you to disable the prompt
if you frequently checkout untracked branches
and don't want to be prompted to track them.

**Accepted values:**

- `true` (default)
- `false`

### spice.branchPrompt.sort

<!-- gs:version v0.11.0 -->

Commands like $$gs branch checkout$$, $$gs branch onto$$, etc.,
that require a branch name will present an interactive prompt
to select the branch when one isn't provided.

This option controls the sort order of branches in the prompt.
It is git-spice's equivalent of
[git's branch.sort configuration](https://git-scm.com/docs/git-config#Documentation/git-config.txt-branchsort).

Commonly used values are:

- `committerdate`: sort by commit date
- `refname`: sort by branch name (default)
- `authordate`: sort by author date

See [git-for-each-ref(1) field names](https://git-scm.com/docs/git-for-each-ref#_field_names)
for a full list of available fields.

Prefix a field name with `-` to sort in descending order.
For example, use `-committerdate` to sort by commit date in descending order.

### spice.branchCreate.commit

<!-- gs:version v0.5.0 -->

Whether $$gs branch create$$ should commit staged changes to the new branch.
Set this to `false` to default to creating new branches without committing,
and use the `--commit` flag to commit changes when needed.

- `true` (default)
- `false`

### spice.branchCreate.prefix

<!-- gs:version v0.14.0 -->

If set, the prefix will be prepended to the name of every branch created 
with $$gs branch create$$.

Commonly used values are:

- `<name>/`: the committer's name
- `<username>/`: the committer's username

### spice.checkout.verbose

<!-- gs:version v0.16.0 -->

Whether branch navigation commands
($$gs up$$, $$gs down$$, $$gs top$$, $$gs bottom$$,
$$gs trunk$$, $$gs branch checkout$$)
should print a message when switching branches.

**Accepted values:**

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

### spice.forge.gitlab.apiUrl

<!-- gs:version v0.13.0 -->

URL at which the GitLab API is available.
Defaults to `$GITLAB_API_URL` if set, or the GitLab URL otherwise.

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

### spice.log.crFormat

<!-- gs:version v0.13.0 -->

Whether $$gs log short$$ and $$gs log long$$ should show the CR URL or the ID next
to any remote branches.

**Accepted values:**

- `"url"`: show the CR URL
- `"id"`: (default) show the CR ID

### spice.logShort.crFormat

<!-- gs:version v0.15.0 -->

Override for $$gs log short$$ to control how change requests are displayed.
If not set, falls back to `spice.log.crFormat`.

**Accepted values:**

- `"url"`: show the CR URL
- `"id"`: show the CR ID

### spice.logLong.crFormat

<!-- gs:version v0.15.0 -->

Override for $$gs log long$$ to control how change requests are displayed.
If not set, falls back to `spice.log.crFormat`.

**Accepted values:**

- `"url"`: show the CR URL
- `"id"`: show the CR ID

### spice.log.pushStatusFormat

<!-- gs:version v0.13.0 -->

Whether $$gs log short$$ and $$gs log long$$ should show
whether the branch is in sync with its pushed counterpart.

**Accepted values:**

- `true` (default): show the push status
- `false`: don't show the push status
- `"aheadBehind"`:
  show the number of outgoing and incoming commits in the form `⇡1⇣2`,
  where `⇡` indicates outgoing commits and `⇣` indicates incoming commits

### spice.rebaseContinue.edit

<!-- gs:version v0.10.0 -->

Whether $$gs rebase continue$$ should open an editor to modify the commit message
when continuing after resolving a rebase conflict.

If set to false, you can opt in to opening the editor with the `--edit` flag.

**Accepted values:**

- `true` (default)
- `false`

### spice.submit.draft

<!-- gs:version v0.16.0 -->

Default value for the `--draft`/`--no-draft` flag when creating new change
requests with $$gs branch submit$$ and friends.

```freeze language="terminal" float="right"
{green}${reset} git config {red}spice.submit.draft{reset} {mag}true{reset}
{green}${reset} gs branch submit{reset}
{gray}# ...{reset}
{green}Draft{reset}: [{mag}Y{reset}/{mag}n{reset}]
{gray}Mark the change as a draft?{reset}

{green}${reset} git config {red}spice.submit.draft{reset} {mag}false{reset}
{green}${reset} gs branch submit{reset}
{gray}# ...{reset}
{green}Draft{reset}: [{mag}y{reset}/{mag}N{reset}]
{gray}Mark the change as a draft?{reset}
```

This option affects both interactive and non-interactive modes:

- In *non-interactive mode*, setting this value to true
  will cause git-spice to assume `--draft` when creating new change requests.
- In *interactive mode*, this value is used as the default for the prompt
  asking whether to create the change request as a draft.

**Accepted values:**

- `true`: create CRs as drafts by default
- `false` (default): create CRs as ready for review by default

### spice.submit.label

<!-- gs:version v0.16.0 -->

Add the configured labels to all submitted and updated change requests
when using $$gs branch submit$$ and friends.

The value must be a comma-separated list of labels.

Labels specified with the `-l`/`--label` flags
will be combined with the configured labels.

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

### spice.submit.template

<!-- gs:version unreleased -->

Template to use when submitting a change request with $$gs branch submit$$,
and multiple templates are available.
If set, this template will be selected without prompting the user to pick one.

The value should match the filename of one of the available templates
(e.g., `PULL_REQUEST_TEMPLATE.md`).

**Example:**

```bash
git config spice.submit.defaultTemplate "PULL_REQUEST_TEMPLATE.md"
```

When this is configured and multiple templates exist,
git-spice will automatically use the specified template
without prompting the user for selection.

### spice.submit.navigationComment

Specifies whether CR submission commands ($$gs branch submit$$ and friends)
should post or update a navigation comment to the CR.

**Accepted values:**

- `true` (default): always post or update navigation comments
- `false`: don't post or update navigation comments
- `multiple`:
  post or update navigation comments only for stacks with at least two CRs

### spice.submit.navigationCommentSync

<!-- gs:version v0.16.0 -->

Controls which branches' navigation comments are synced (created or updated)
when submitting branches.

**Accepted values:**

- `branch` (default): sync navigation comments only for the submitted branches
- `downstack`: sync navigation comments for all downstack branches

When set to `branch`, if a branch stacked on top of another branch
is submitted with $$gs branch submit$$,
only the navigation comment for the submitted branch will be updated.
If both branches are submitted (e.g. with $$gs downstack submit$$),
then both branches' navigation comments will be updated.

When set to `downstack`, any time a branch is submitted,
git-spice will update the navigation comment for that branch
and all branches below it in the stack.

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

### spice.submit.updateOnly

<!-- gs:version unreleased -->

Whether multi-branch submission commands ($$gs stack submit$$ and friends)
should assume --update-only mode by default.

If true, submit operations will only update existing branches by default.
Use the --no-update-only flag to override this behavior.

This option has no effect on $$gs branch submit$$,

**Accepted values:**

- `true`
- `false` (default)

### spice.submit.web

<!-- gs:version v0.8.0 -->

Whether submission commands ($$gs branch submit$$ and friends)
should open a web browser with submitted CRs.

<!-- gs:version v0.16.0 --> If set to `created`,
git-spice will open the web browser only for newly created CRs,
and not for existing ones that were updated.

**Accepted values:**

- `true`
- `false` (default)
- `created` (<!-- gs:version v0.16.0 -->)
