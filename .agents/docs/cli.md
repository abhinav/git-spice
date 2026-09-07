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

## User-Facing Logs

Log messages are part of the standard-error interface.
Write them for a user following the operation,
not as an internal trace of the implementation.

We use two forms of logging based on purpose:
structured logs and printf-style logs.

### Structured Logs

Structured logs use full sentences (capitalized, punctuated, and complete)
with structured attributes for dynamic values.
Use these when reporting a single event, or for debug-level logs.

```go
log.Warn(
	"Could not load remote change data. Using local data.",
	"changeID", changeID,
	"error", err,
)
```

Structured log attributes get user-friendly camelCase keys.
Errors use the `"error"` key.
When an error is recoverable,
state the fallback or consequence in the message.

### printf-style Logs

printf-style logs use a lowercase prefix for the subject of the message,
then a colon and a lowercase statement of the action or result.
Use these when repeated messages report progress for individual items.

```go
log.Infof("%v: restacked onto %v", branch, onto)
log.Infof("%v: anchor moved to %v", draft.ID, anchor)
```

Exception: Debug level logs always use structured logging.

#### printf formatting verbs

Use `%v` when Go's default formatting is the intended presentation,
including for strings and integers.
Use another formatting verb only when its distinct presentation
is part of the output contract,
such as quoting, a non-decimal base, padding, or precision.
Use `%q` for quoted strings.

Use `WithPrefix` for stable subsystem context,
such as a forge or a long-running operation.
Keep dynamic branch, change, draft, and other item identities
in the message or structured attributes.

## Generated Files

Run `mise run generate`
after changing commands, flags, configuration, or command help.
This updates generated CLI references
and related generated artifacts.

Document user-facing command changes in `doc/src`
when the website should explain the behavior.
