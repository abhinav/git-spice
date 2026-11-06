# Configuration will use git-config

Thus far, git-spice hasn't provided much in terms of configuration dials.
Behavior is either derived from Git configuration or doesn't have flexibility.
Examples of places where we need configuration include:

- Whether to post a stack visualization comment on PRs.
  Right now, we do this unconditionally.
  We'd like for users to be able to turn this off,
  or have it be posted only of there are at least two branches in the stack.
- Ability to add a prefix to all created branch names--possibly derived
  from an external command.
- Support for custom shorthands in addition to built-ins.

To support this, we'll need a configuration system.
The usual discussions around YAML, TOML, etc. could be had,
but given that Git is a pre-requisite for git-spice,
we can leverage `git-config`.

The following flags can be used for the bulk of the work here.

    --get-regexp <name-pattern>
    --null

Example configuration keys:

- `spice.submit.navigationComment`: true, false, multiple
- `spice.create.branchPrefix`: prefix for new branches
- `spice.alias.*`: custom aliases and shorthands

Note that regardless of configuration system in use,
custom short hands will be special cased:
while most configuration options will have flag-level analogs,
shorthands will not as we expand them before parsing command line flags.
