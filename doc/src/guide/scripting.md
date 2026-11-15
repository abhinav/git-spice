---
title: Scripting
icon: material/script-text-outline
description: Use git-spice in scripts and automate workflow steps.
---

# Scripting and automation

git-spice attempts to be compatible with scripts and automated workflows.

## Non-interactive usage

All commands support `--no-prompt` to disable interactive prompts.

With prompts disabled,
actions that previously prompted for a choice
will default to the safest option.

If git-spice encounters a choice it cannot make automatically,
it exits with an error.
For example, `gs up --no-prompt` fails if there are multiple branches
stacked directly above the current one.

### Submitting non-interactively

<!-- gs:version unreleased -->

To submit new change requests in non-interactive mode,
supply the title and body in advance.
Use `--draft` or `--no-draft` to set the draft status for all new CRs.

The title and body can be supplied with the `--message-file` option.
The flag is accepted in two forms:

- `--message-file PATH` for $$gs branch submit$$
- `--message-file BRANCH:PATH` for batch submission commands
  ($$gs stack submit$$, $$gs upstack submit$$, $$gs downstack submit$$)

`PATH` specifies a file containing the message.
The first line of the file is used as the title,
and the remaining text is used as the body (optional).

See [Submitting change requests](cr.md) for the rest of the submission workflow.
