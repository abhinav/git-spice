---
title: Experiments
icon: material/test-tube
description: >-
  Enabling experimental features in git-spice.
---

# Experiments

git-spice includes experimental features
that can be enabled on an opt-in basis.

!!! warning "Before you use experiments"

    Be aware that experimental features:

    - may be incomplete or buggy
    - may change or be removed in future releases
    - may not be well-documented
    - may destroy your work

```freeze language="terminal" float="right"
{gray}# Enable an experiment{reset}
{green}${reset} git config {red}spice.experiment.<name>{reset} {mag}true{reset}

{gray}# Disable an experiment{reset}
{green}${reset} git config {red}spice.experiment.<name>{reset} {mag}false{reset}
```

Experiments are enabled with `git config`,
by setting `spice.experiment.<name>` to `true` or `false`.

If you use an experimental feature,
feel free to report issues and provide feedback about them.

## Available experiments
