# Documentation And Release Notes

Use this guide when changing Markdown documentation,
generated CLI reference content, changelog entries, release notes,
or other user-facing prose.

## Documentation

Website documentation lives in `doc/src`.
The website layout is configured by `doc/mkdocs.yml`.

Document user-facing behavior when users need the website
to understand a command, configuration value, workflow, or limitation.

When documenting an unreleased feature, add the placeholder:

```markdown
<!-- gs:version unreleased -->
```

The release process converts this placeholder
into the released version number at release time.

Use `$$...$$` only for command names
and configuration names that `doc/hooks/cliref.py` can link.
Do not include flags inside `$$...$$`.

```markdown
Use $$gs repo sync$$ to update tracked branches.
Run `gs repo sync --restack` to restack after syncing.
```

## Prose Style

Write external prose for readers
who do not have the conversation, tool output, or implementation history.

State the user-visible behavior first.
Use implementation details only when readers need them
to understand, evaluate, or safely change the result.

Use semantic line breaks in Markdown, multi-line comments, commit messages,
and changelog prose.
Keep Markdown prose near 80 columns
and never exceed 100 columns
unless a link or code span requires it.

## Generated Reference

Run `mise run generate`
after changing commands, flags, configuration, or help text.
Generated outputs include CLI reference content
such as `doc/includes/cli-reference.md`.

## Changelog Entries

git-spice uses Changie.
Unreleased entries live under `.changes/unreleased`.

Create entries through the repository wrapper:

```bash
mise run changie new --kind $kind --body $body
```

Do not hand-write a Changie file
unless the wrapper cannot be used
and the user approves the fallback.

Add a changelog entry for user-facing changes:

- new features
- changes to existing behavior
- bug fixes with visible user impact
- deprecations
- removals
- security fixes

Do not add a changelog entry for internal-only changes:

- refactors with no behavior change
- test-only changes
- CI or tooling-only changes
- code movement with no user-facing effect

## Changelog Kinds

Choose the kind from `.changie.yaml`
based on the user-visible outcome:

| Kind | Use for |
| --- | --- |
| `Added` | New user-facing capability |
| `Changed` | Changed behavior or workflow |
| `Deprecated` | Newly discouraged capability |
| `Removed` | Removed capability |
| `Fixed` | Bug fix with visible user impact |
| `Security` | Security fix |

## Changelog Body

Describe the user-facing effect, not the implementation.
Use passive voice.

For component-specific changes, prefix the body with the command or domain,
such as `submit:`, `repo sync:`, or `github:`.

Good:

```text
repo sync: Branches are restacked after syncing when restack is enabled.
```

Weak:

```text
refactor repo sync restack helper
```

If a PR has no changelog entry because the change is internal,
include a trailer in the PR description:

```text
[skip changelog]: Internal refactor with no user-facing behavior change.
```
