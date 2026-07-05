# Development Workflow

## Task Guides

Read the relevant guide before editing that kind of code or prose:

| Task | Guide |
| --- | --- |
| Package boundaries, dependencies, constructors, APIs | `.agents/docs/design.md` |
| Go implementation style and symbol ordering | `.agents/docs/style.md` |
| Code comments and symbol documentation | `.agents/docs/comments.md` |
| Command behavior, flags, output, generated CLI docs | `.agents/docs/cli.md` |
| Unit tests, test scripts, mocks, regression tests | `.agents/docs/testing.md` |
| Documentation, changelog, release-facing prose | `.agents/docs/docs-and-release.md` |
| Branches, commits, stacks, PR publishing boundaries | `.agents/docs/git-workflow.md` |

Before changing files or Git state,
verify the working directory
and preserve unrelated dirty,
staged,
and untracked files.

`internal/handler/AGENTS.md` contains additional rules
for handler packages.
Read it before adding or changing handler code.

## Quick Reference

| Step | Command |
| --- | --- |
| Verify code compiles | Use `mcp__gopls__go_diagnostics` if available |
| Build project | `mise run build` |
| Update generated files | `mise run generate` |
| Run linters | `mise run lint` |
| Format code | `mise run fmt` |
| Run all tests | `mise run test` |
| Run specific unit tests | `go test ./path/to/package -run TestRegex` |
| Run all test scripts | `mise run test:script` |
| Run specific test script | `mise run test:script --run $name` |
| Update test script | `mise run test:script --run $name --update` |
| Add changelog entry | `mise run changie new --kind $kind --body $body` |

Use broad test commands sparingly during development.
Prefer the smallest command that exercises the changed behavior.

## Development Loop

For features and bug fixes:

1. Add or update the test first.
   For a bug, verify that the regression test fails before the fix.
2. Implement the change.
3. Run the narrowest relevant test command.
4. Run `mise run fmt`.
5. Run `mise run lint`.
6. Run `mise run build`.
7. Run `mise run generate`
   when commands, flags, configuration, or generated docs change.
8. Add a changelog entry when the change is user-facing.

## Design Summary

Organize packages by domain responsibility.
A package should expose operations in its domain vocabulary,
not collect unrelated helpers or framework wiring.

Handler packages coordinate user-facing command workflows.
Command packages parse syntax and construct typed requests.
Lower-level packages own local invariants and domain operations.

Keep infrastructure concerns at boundaries.
Read environment variables, configuration files,
and process state at explicit entry points.
Pass typed values or dependencies to the component
that owns their lifetime.

Do not shell out as an implementation shortcut.
When behavior depends on another process,
wrap that process behind a concrete API
whose methods describe git-spice operations.

Do not introduce new third-party dependencies without explicit approval.
