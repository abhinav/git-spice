---
icon: material/scissors-cutting
title: Shorthands
description: >-
  Most commands come with intuitive shorthand variants
  that are easy to remember and type.
---

# CLI shorthands

git-spice offers short versions of most commands
to make them easier to remember and type.

To determine the shorthand for a command,
run the command with the `--help` flag.

```freeze language="terminal"
{green}${reset} gs branch create --help
Usage: gs branch (b) create (c) [<name>] [flags]

{gray}# ...{reset}
```

The shorthand is built by joining the bits in parentheses.
For example, the shorthand for $$gs branch create$$ above is `gs bc`.
Another example:

```freeze language="terminal"
{green}${reset} gs branch checkout --help
Usage: gs branch (b) checkout (co) [<branch>] [flags]

{gray}# ...{reset}
```

The shorthand for $$gs branch checkout$$ is `gs bco`.

## When to use shorthands?

We encourage adopting the command shorthands
after you are comfortable with the corresponding full command names.
The shorthands are designed to be easy to remember and type,
but they may not be obvious if you don't know the full command name.

The shorthands act as a mnemonic aid:
invoke the full command name in your head while typing the shorthand.

## Available shorthands

--8<-- "cli-shorthands.md"
