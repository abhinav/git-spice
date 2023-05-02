# 5. Submit commit range

Date: 2023-06-25

## Status

Accepted

## Context

The submit command needs to know what range of commits
it should submit--create or update PRs for.
We need to define the default range for it,
as well as what customizations it allows.

## Decision

Initially, we will use the `@{upstream}` shorthand provided by Git
to get the upstream.
For example:

```bash
git rev-list HEAD --not @{upstream}
```

## Consequences

This will only work for branches that have an upstream configured.
For branches that do not have an upstream configured,
users will have to do something like:

```bash
git branch --set-upstream-to=origin/main
```

This limitation can be addressed in the future.
