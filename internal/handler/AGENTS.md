# Handler Patterns

Handler packages coordinate user-facing workflows
between command code and lower-level repository operations.

A handler should usually own the sequence of operations
that makes a command feel like one operation:
loading state,
calling service or Git methods,
coordinating other handlers,
emitting user-facing log messages,
and arranging continuation after an interrupted rebase.

Lower-level packages should still own their local invariants.
For example,
a service method can update branch state or perform one Git operation,
while a handler decides how that operation fits into a command workflow.

```go
type Handler struct {
	Log     *silog.Logger // required
	Service Service       // required
	Other   OtherHandler  // required
}

func (h *Handler) RunThing(ctx context.Context, req *ThingRequest) error {
	// Load command-level context.
	// Coordinate lower-level operations.
	// Emit messages that describe the user-visible workflow.
	return nil
}
```

## Dependencies

Handlers usually define small interfaces
for the dependencies they call directly.
This keeps tests focused on the handler's coordination logic
and documents which part of a larger package the handler actually needs.

Use concrete implementations at construction time,
but keep the handler field typed as the local interface
when the handler only needs a subset of the dependency.

```go
type Repository interface {
	ReadThing(ctx context.Context, name string) (*Thing, error)
	WriteThing(ctx context.Context, req WriteThingRequest) error
}

var _ Repository = (*git.Repository)(nil)

type Service interface {
	LookupBranch(ctx context.Context, branch string) (*Branch, error)
}

var _ Service = (*spice.Service)(nil)

type Handler struct {
	Log        *silog.Logger // required
	Repository Repository   // required
	Service    Service      // required
}
```

Handler-to-handler dependencies follow the same shape.
Define a narrow interface for the behavior being composed,
rather than reaching through another handler's concrete type.

```go
type RestackHandler interface {
	RestackBranch(ctx context.Context, branch string) error
}

type Handler struct {
	Restack RestackHandler // required
}
```

## Requests

Handler methods usually accept a request struct.
The request should describe one invocation of the operation:
branch names,
destination names,
selected modes,
callbacks,
and other values that may differ between calls
within the same process.

Do not put dependencies such as repositories,
worktrees,
stores,
or services on request structs.
Those belong on the handler.

```go
type MoveRequest struct {
	// Branch is the branch to move.
	Branch string // required

	// Onto is the destination branch.
	Onto string // required

	// Mode selects how related branches are handled.
	Mode MoveMode
}

func (h *Handler) MoveBranch(ctx context.Context, req *MoveRequest) error {
	// ...
}
```

## Options

`Options` types expose command-line flags or configuration
from the handler layer.

Name the type for the operation it configures,
especially when the handler has more than one entrypoint.
The handler request carries the options as an optional field.

```go
type MoveOptions struct {
	DryRun bool `short:"n" help:"Print what would happen"`
}

type MoveRequest struct {
	Branch string // required

	Options *MoveOptions // optional
}

func (h *Handler) MoveBranch(ctx context.Context, req *MoveRequest) error {
	opts := cmp.Or(req.Options, &MoveOptions{})
	// ...
}
```

Commands commonly embed the handler's options type,
letting Kong populate those fields directly.
The command then passes the populated options
through the handler request.

```go
type moveCmd struct {
	handler.MoveOptions

	Branch string `arg:""`
}

func (cmd *moveCmd) Run(ctx context.Context, h MoveHandler) error {
	return h.MoveBranch(ctx, &handler.MoveRequest{
		Branch:  cmd.Branch,
		Options: &cmd.MoveOptions,
	})
}
```

Use this pattern only when the handler owns behavior
that should be exposed as command-line flags or configuration.
Do not introduce an `Options` type
only to group ordinary request fields.

## Construction

Most handlers are constructed by Kong dependency providers
near the application wiring.
This is the normal place to connect concrete repositories,
worktrees,
stores,
services,
and other handlers to the local interfaces
defined by the handler package.

```go
kctx.BindSingletonProvider(func(
	log *silog.Logger,
	repo *git.Repository,
	svc *spice.Service,
	other OtherHandler,
) (ThingHandler, error) {
	return &thing.Handler{
		Log:        log,
		Repository: repo,
		Service:    svc,
		Other:      other,
	}, nil
})
```

Use command-local provider wiring
when construction depends on command-specific setup
or when the handler is only needed by that command path.

```go
func (cmd *thingCmd) AfterApply(kctx *kong.Context) error {
	return kctx.BindToProvider(func(
		log *silog.Logger,
		svc *spice.Service,
	) (ThingHandler, error) {
		return &thing.Handler{
			Log:     log,
			Service: svc,
		}, nil
	})
}
```

Prefer the shared provider path
when the same handler is used by multiple commands.

## Composition

Handlers can depend on other handlers
when a workflow needs to reuse another command-level operation.
Use a narrow interface for the behavior being composed,
and call that interface from the coordinating handler.

```go
type CleanupHandler interface {
	CleanupBranch(ctx context.Context, branch string) error
}

type Handler struct {
	Service Service        // required
	Cleanup CleanupHandler // required
}

func (h *Handler) FinishThing(ctx context.Context, req *FinishRequest) error {
	if err := h.Service.MarkDone(ctx, req.Branch); err != nil {
		return err
	}

	return h.Cleanup.CleanupBranch(ctx, req.Branch)
}
```

This is most useful when the composed behavior
has its own workflow concerns,
such as logging,
restacking,
autostash,
tracking,
or rescue handling.
If the operation is only a lower-level state or Git primitive,
prefer depending on the lower-level service or Git interface directly.

## Rescue And Continuation

Handlers that run operations capable of entering rebase rescue
should preserve enough per-invocation information
to resume the same command semantics
after the user resolves conflicts.

Carry that information on the request,
then pass it to the service rescue operation
at the boundary where the interrupting error is handled.

```go
type MoveRequest struct {
	Branch string // required
	Onto   string // required

	// ContinueCommand resumes this operation
	// after an interrupted rebase.
	ContinueCommand []string // required
}

func (h *Handler) MoveBranch(ctx context.Context, req *MoveRequest) error {
	if err := h.Service.MoveBranch(ctx, req.Branch, req.Onto); err != nil {
		return h.Service.RebaseRescue(ctx, RescueRequest{
			Err:     err,
			Command: req.ContinueCommand,
			Branch:  req.Branch,
			Message: "interrupted while moving branch",
		})
	}

	return nil
}
```

Keep the continuation command tied to the operation being resumed.
If flags or modes affect the resumed behavior,
include them in the continuation command supplied by the caller.
