# Command-Line Interfaces

Use this guide when changing command behavior, flags, configuration, output,
or generated CLI documentation.

## Commands Are Adapters

A command parses command-line syntax, builds a typed request,
calls a handler or domain operation, and translates the result
into the command output contract.

Do not make command implementations a second business-logic layer.
Policy, state transitions, and reusable workflows belong
in handlers or domain packages.

```go
// Good: command syntax is converted into a handler request.
func (cmd *moveCmd) Run(ctx context.Context, h MoveHandler) error {
	return h.MoveBranch(ctx, &move.MoveRequest{
		Branch:  cmd.Branch,
		Onto:    cmd.Onto,
		Options: &cmd.MoveOptions,
	})
}
```

```go
// Bad: command code owns workflow policy.
func (cmd *moveCmd) Run(ctx context.Context, repo *git.Repository) error {
	if cmd.Restack {
		// ...
	}
	return repo.MoveBranch(ctx, cmd.Branch, cmd.Onto)
}
```

## Handler Boundary

Most command workflows should cross into `internal/handler/...`.
Handlers coordinate user-facing operations
between command code and lower-level repository behavior.

Commands commonly embed handler option types,
then pass the populated options through a request.
Read `internal/handler/AGENTS.md`
before adding or changing that pattern.

## Typed Requests

Convert flags, arguments, and configuration into typed values
before leaving the command boundary.
Do not pass parser structs, raw flag maps,
or arbitrary command-line argument slices into lower layers.

```go
return h.MoveBranch(ctx, &handler.MoveRequest{
	Branch:  cmd.Branch,
	Onto:    cmd.Onto,
	Options: &cmd.MoveOptions,
})
```

## Output Contracts

Treat standard output and standard error as separate interfaces.
Use standard output for the requested result,
especially data intended for pipes, files,
or machine consumption.

Use standard error for diagnostics, progress, and logs.
Structured output must not contain incidental log lines.

Rendering belongs at the command or handler boundary.
Domain operations should not need to know
whether a result will be printed as text, JSON, or another representation.

## Generated Files

Run `mise run generate`
after changing commands, flags, configuration, or command help.
This updates generated CLI references
and related generated artifacts.

Document user-facing command changes in `doc/src`
when the website should explain the behavior.
