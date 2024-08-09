---
icon: material/scissors-cutting
title: Shorthands
description: >-
  Most commands come with intuitive shorthand variants
  that are easy to remember and type.
---

# CLI shorthands

git-spice comes built-in with short versions of most commands
to make them easier to remember and type.
To determine the shorthand for a command,
run the command with the `--help` flag.

```freeze language="terminal"
{green}${reset} gs branch create --help
Usage: gs branch (b) create (c) [<name>] [flags]

{gray}# ...{reset}
```

The shorthand for a command is the bits in parentheses joined together.
For example, the shorthand for $$gs branch create$$ above is `gs bc`.
Another example:

```freeze language="terminal"
{green}${reset} gs branch checkout --help
Usage: gs branch (b) checkout (co) [<branch>] [flags]

{gray}# ...{reset}
```

The shorthand for $$gs branch checkout$$ is `gs bco`.

!!! question "When to use built-in shorthands?"

    We encourage adopting the built-in shorthands
    after you are comfortable with the corresponding full command names.
    The shorthands are designed to be easy to remember and type,
    but they may not be obvious if you don't know the full command name.

    The shorthands act as a mnemonic aid:
    invoke the full command name in your head while typing the shorthand.

## Built-in shorthands

Below is a complete list of shorthands built into git-spice.

--8<-- "cli-shorthands.md"

## Custom shorthands

<!-- gs:version v0.4.0 -->

git-spice's [configuration system](config.md) supports defining
custom shorthands for git-spice commands
by setting configuration keys under the `spice.shorthand` namespace.

    spice.shorthand.<short> = <command> <arg> ...

Shorthands begin with the name of a git-spice command,
followed by zero or more arguments to the command.

For example:

```freeze language="terminal"
{gray}# Define a shorthand for the current user{reset}
{green}${reset} git config --global spice.shorthand.ch "branch checkout"

{gray}# Define a shorthand in just the current repository{reset}
{green}${reset} git config --local spice.shorthand.can "commit amend --no-edit"
```

### Overriding built-in shorthands

User-defined shorthands take precedence over built-in shorthands.
You may use this to override a built-in shorthand with a custom one.
For example:

```freeze language="terminal"
{gray}# Replace the "branch restack" shorthand{reset}
{green}${reset} git config --global spice.shorthand.br "branch rename"
```

If the result of a user-defined shorthand refers to a built-in shorthand,
both will be expanded.

```freeze language="terminal"
{green}${reset} git config --global spice.shorthand.bb bco
{gray}# bb will expand to bco, which will expand to "branch checkout"{reset}
```
