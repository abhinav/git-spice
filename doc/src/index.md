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
conveniently modify and rebase them, and create
<!-- gs:github --> *Pull Requests* or
<!-- gs:gitlab --> *Merge Requests* from them.

It works with Git instead of trying to replace Git.
Introduce it in small places in your existing workflow
without changing how you work wholesale.

<div class="grid" markdown>

```freeze language="shell"
# Create a branch
$ git checkout -b feat1
$ gs branch track
# Or
$ gs branch create feat1

# Restack a branch
$ git rebase -i base
# Or
$ gs branch restack
```

```freeze language="shell"
# Restack all branches
$ gs stack restack

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

    See also [:material-frequently-asked-questions: FAQ > What is stacking?](community/faq.md#what-is-stacking)

[:material-run: Get started](start/index.md){ .md-button }
[:material-compass: Learn more](guide/index.md){ .md-button }

## Features

<div class="grid cards" markdown>

-   [:octicons-git-branch-16:{ .lg .middle } __Manage local branches__](guide/branch.md)

    ---

    Create, edit, and navigate stacks of branches with ease.
    With git-spice's branch management commands,
    you can keep your stack in sync with the trunk branch,
    automatically rebase dependent branches, and more.

-   [:octicons-git-pull-request-16:{ .lg .middle } __Submit change requests__](guide/cr.md) <!-- gs:github --> <!-- gs:gitlab -->

    ---

    Submit branches in your stack with a single command.
    git-spice can submit
    [the current branch](cli/reference.md#gs-branch-submit),
    [the entire stack](cli/reference.md#gs-stack-submit), or
    [parts of](cli/reference.md#gs-upstack-submit)
    [the stack](cli/reference.md#gs-downstack-submit).
    If a branch has already been submitted, git-spice will update the submission.
    If it has been merged, git-spice will automatically restack branches that depend on it.

-   [:material-stairs:{ .lg .middle } __Incremental improvements__](start/stack.md)

    ---

    git-spice does not need to be adopted all at once.
    It does not expect you to flip your entire workflow upside down.
    Incorporate it into your workflow at your own pace,
    one feature at a time.

-   [:material-cloud-off-outline:{ .lg .middle } __Offline-first__](guide/internals.md)

    ---

    git-spice operates entirely locally.
    It talks directly to Git, and when you ask for it, to GitHub/GitLab.
    All state is stored locally in your Git repository.
    A network connection is not required, except when pushing or pulling.

-   [:material-lightbulb-on:{ .lg .middle } __Intuitive shorthands__](cli/shorthand.md)

    ---

    Most commands in git-spice have easy-to-remember shorthands.
    For example, $$gs branch create$$ can be shortened to $$gs branch create|gs bc$$.
    Explore the list of shorthands with `--help` or at [Shorthands](cli/shorthand.md).
    We recommend adopting these incrementally.

-   [:material-currency-usd-off:{ .lg .middle } __Free and open-source__](https://github.com/abhinav/git-spice)

    ---

    git-spice is free and open-source software.
    It is made available under the GPL-3.0 license.
    You may use it to develop proprietary software,
    but any changes you make to git-spice itself must be shared.

</div>
