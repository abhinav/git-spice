---
icon: material/help-circle
title: Troubleshooting
description: >-
  Common issues and solutions when using git-spice
  with various Git configurations and environments.
---

This page covers common issues you may encounter while using git-spice
and their solutions.

## `fatal: Cannot rebase onto multiple branches.`

$$gs repo sync$$ may fail with the following error intermittently:

```
fatal: Cannot rebase onto multiple branches.
```

If the command succeeds on retry,
the error is likely caused by background Git processes
that are running concurrently with git-spice operations.

Common sources of this include:

- Shell plugins configured to automatically fetch Git repository updates
- Editors or IDEs that automatically sync Git repositories
- Git GUI clients that run background synchronization tasks

To fix this, disable automatic Git fetching for your environment.
Common solutions include:

- **Pure Zsh**: Add the following to your `.zshrc`:

    ```bash
    export PURE_GIT_PULL=0
    ```

- **VS Code**: add the following to your `settings.json`:

    ```json
    "git.autofetch": false
    ```
