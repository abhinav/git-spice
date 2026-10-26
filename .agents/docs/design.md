# Design

Use this guide when changing package boundaries, handler responsibilities,
dependencies, constructors, or cross-package APIs.

## Package Responsibility

Organize Go packages by domain responsibility.
Each package should own one cohesive part of git-spice
and expose operations in that domain's vocabulary.

Avoid packages that collect unrelated helpers, declaration kinds,
or framework wiring without owning a domain.
Purpose-scoped utility packages are permitted
when the domain is otherwise taken or misleading,
such as `sliceutil` when `slices` would conflict
with the standard library package.

Good package names describe the responsibility:

```text
internal/branch
internal/forge/github
internal/handler/submit
```

Weak package names hide the responsibility:

```text
internal/helper
internal/service
internal/type
```

## File Organization

Organize files by granular domain responsibility.
Keep a concept near the constructors,
methods, private interfaces, and helpers that make the concept useful.

Do not split files merely by declaration kind
or by arbitrary size targets.
Move code when the move improves context-locality for a future change.

## Dependency Direction

Abstractions flow in one direction:

```text
command syntax -> handler workflow -> domain operation -> infrastructure
```

Command packages parse command-line syntax
and build typed requests.
Handler packages coordinate command workflows.
Domain packages own state transitions and invariants.
Infrastructure packages wrap Git, forges, filesystems, processes,
and other external systems.

Lower layers must not depend on command parser types,
generated command structs, process environment lookups,
or caller-specific output formats.

## Handler Packages

Handler packages are command-workflow coordinators.
They load command-level context, call lower-level services or Git APIs,
compose other handlers when a workflow needs another workflow,
emit user-facing log messages, and arrange rescue or continuation behavior.

Read `internal/handler/AGENTS.md`
before adding or changing handler code.
In summary:

- Handler fields hold dependencies.
- Handler requests hold values for one invocation.
- Handler `Options` types represent optional flags or configuration.
- Handlers define narrow local interfaces
  for the dependency behavior they call directly.
- Lower-level packages still own their local invariants.

## Dependency Wiring

Use Kong bindings for application-scoped dependencies.
An application-scoped dependency is any abstraction whose identity is fixed
after CLI arguments are parsed:
repositories, worktrees, stores, services, handlers, forge registries,
loggers, and other process-lifetime collaborators.

Kong initializes bindings lazily,
so register application-scoped dependencies with Kong
even when only some commands use them.

Use command-local providers only when construction depends on parsed command
arguments or command-specific setup.

Kong belongs at the command and application wiring boundary.
Do not pass Kong contexts, parser values,
or provider mechanics into handlers, domain packages,
or infrastructure packages.

Handlers may define narrow local interfaces.
Kong provider functions connect concrete implementations to those interfaces.

## Process Boundaries

Do not shell out casually.
When git-spice needs another process,
wrap that process behind a concrete library-like API.
Callers should invoke domain-shaped methods,
not construct arbitrary command-line argument slices.

```go
// Good: callers express the operation.
branch, err := repo.LookupBranch(ctx, name)
if err != nil {
	return fmt.Errorf("lookup branch %q: %w", name, err)
}
```

```go
// Bad: callers leak process construction across the boundary.
out, err := runner.Run(ctx, "git", "branch", "--list", name)
```

If a lower layer accepts raw arguments,
the caller must understand process syntax, escaping, output parsing,
and error interpretation.
That is a boundary failure.

## Configuration And Global State

Do not introduce mutable global state.
Read environment variables, configuration files,
and other external process state at an entry point
or explicit infrastructure boundary.

Pass typed values or dependencies to the component
that owns their lifetime.
Values that change per operation belong on a request or parameter.
Values that stay fixed for an object lifetime belong on the object.

## Constructors And Dependencies

Use a constructor when construction establishes behavior:
validation, normalization, implementation selection, resource acquisition,
or another invariant.

Do not add a constructor that only copies dependencies into fields.
When no construction behavior is needed,
prefer direct struct initialization
with required dependencies marked inline.

```go
type Handler struct {
	Log     *silog.Logger // required
	Service Service       // required
}
```

When construction needs several required inputs
or the input set is likely to grow,
use a `Config` struct and a `New(Config)` constructor.
Mark required `Config` fields inline.

```go
type Config struct {
	Log     *silog.Logger // required
	Service Service       // required
}

func New(config Config) (*Handler, error) {
	if config.Log == nil {
		return nil, errors.New("log is required")
	}
	return &Handler{
		Log:     config.Log,
		Service: config.Service,
	}, nil
}
```

When a constructor has fewer than three required dependencies,
positional parameters are acceptable.
If optional inputs are also needed, accept a trailing `*Options`.

```go
type OpenOptions struct {
	Log *silog.Logger
}

func Open(path string, opts *OpenOptions) (*Repository, error) {
	opts = cmp.Or(opts, &OpenOptions{})
	return openRepository(path, opts.Log)
}
```

Use `Options` only for optional inputs.
A nil `*Options` means defaults.
If a field is required, the type is not an `Options` type.

## Boolean Overuse

Avoid boolean API knobs for behavior choices.
Model finite choices as named modes
and open-ended behavior as an explicit interface.

```go
type RestackMode int

const (
	RestackModeAsk RestackMode = iota
	RestackModeAlways
	RestackModeNever
)
```

## Interfaces

Accept the smallest useful interface at the consumer boundary
when behavior must vary.
Return concrete structs by default.

Do not introduce an interface only to hide one implementation
or make dependencies look uniform.
Concrete dependencies are acceptable
when the concrete API is already the relevant contract,
the value is cheap to construct or pass,
and callers do not need behavioral substitution.

This includes `*silog.Logger`, immutable configuration values,
standard-library values,
and small stateless collaborators.

```go
type BranchLookup interface {
	LookupBranch(ctx context.Context, name string) (*spice.Branch, error)
}

type Handler struct {
	Branches BranchLookup // required
}
```

## Boundary Objects

Prefer parameter and result objects
for package, service, handler, or domain boundaries whose inputs
or outputs are likely to grow.
Keep request and result types next to the operation
that consumes or produces them.
