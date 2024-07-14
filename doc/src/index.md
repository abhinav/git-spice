---
icon: material/home
hide:
  - toc
description: >-
  git-spice is a tool for stacking Git branches.
  It helps you manage and navigate a stack of branches
  that build on top of each other.
---

# git-spice

![](img/logo.png){ align=right width=250px }

git-spice is a tool for stacking Git branches.
It lets you manage and navigate stacks of branches,
conveniently modify and rebase them,
and create GitHub Pull Requests from them.

<div class="grid" markdown>

```freeze language="shell"
# Create a branch
$ gs branch create

# Restack a branck
$ gs branch restack

# Restack all branches
$ gs stack restack
```

```freeze language="shell"
# Submit a PR
$ gs branch submit

# Submit all PRs
$ gs stack submit

# Sync with trunk
$ gs repo sync
```

</div>

!!! question "What is stacking?"

    Stacking refers to the practice of creating branches or pull requests
    that build on top of each other.
    It allows chaining interdependent changes together,
    while still keeping the individual changes small and focused.

    See also [:material-frequently-asked-questions: FAQ > What is stacking?](faq.md#what-is-stacking)

[:material-run: Get started](start/index.md){ .md-button }
[:material-compass: Learn more](guide/index.md){ .md-button }

## Features

<div class="grid cards" markdown>

-   [:octicons-git-branch-16:{ .lg .middle } __Branch management__](guide/branch.md)

    ---

    Create, edit, and navigate stacks of branches with ease.
    With git-spice's branch management commands,
    you can keep your stack in sync with the trunk branch,
    automatically rebase dependent branches, and more.

-   [:octicons-git-pull-request-16:{ .lg .middle } __Pull Request Management__](guide/pr.md)

    ---

    Create GitHub Pull Requests from your stack with a single command.
    git-spice can create
    [a PR for the current branch](cli/index.md#gs-branch-submit),
    [PRs for the entire stack](cli/index.md#gs-stack-submit), or
    [parts of](cli/index.md#gs-upstack-submit)
    [the stack](cli/index.md#gs-downstack-submit).
    If a branch already has a PR, git-spice will update it.
    If a PR is merged,
    git-spice will automatically restack branches that depend on it.

-   [:material-stairs:{ .lg .middle } __Incremental improvements__](start/stack.md)

    ---

    git-spice does not need to be adopted all at once.
    It does not expect you to flip your entire workflow upside down.
    Incorporate it into your workflow at your own pace,
    one feature at a time.

-   [:material-cloud-off-outline:{ .lg .middle } __Offline-first__](guide/internals.md)

    ---

    git-spice operates entirely locally.
    It talks directly to Git, and when you ask for it, to GitHub.
    All state is stored locally in your Git repository.
    A network connection is not required, except when pushing or pulling.

-   [:material-lightbulb-on:{ .lg .middle } __Intuitive shorthands__](cli/shorthand.md)

    ---

    Most commands in git-spice have easy-to-remember shorthands.
    For example, $$gs branch create$$ can be shortened to $$gs branch create|gs bc$$.
    Explore the list of shorthands with `--help` or at [Shorthands](cli/shorthand.md).
    We recommend adopting these incrementally.

</div>
