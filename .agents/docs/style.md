# Style

Use this guide when changing Go implementation code.
Prefer local clarity over broad cleanup.

## Context-Locality

Readable code keeps the context for a change near the code being changed.
A future contributor should not have to cross unrelated files,
helpers, or declarations to understand one operation.

Keep request and result types near their operation.
Keep private interfaces near the consumer.
Keep helper functions near the code they support.
Move shared code only when the shared boundary has the same meaning
for every caller.

## Symbol Ordering

Order Go symbols by narrative dependency, not declaration kind.
A reader should encounter a symbol close to the code
that first makes the symbol useful.

Preferred order:

1. File-wide constants or variables
   that configure the whole file.
2. A cohesive type block:
   type declaration, constructor, then methods.
3. Request, result, mode, and local interface types
   immediately before the operation that uses them.
4. Helper machinery immediately after the operation or group it supports.

Do not collect all constants, interfaces, types, or helpers at the top
of a file
unless they are genuinely file-wide concepts.

## Naming

Use one stable term for one domain concept.
Do not vary names for style.
If two names exist, they should describe a real distinction.

Package names should be singular or compound domain names.
Avoid plural package names unless the name is an established external term.

## Required Fields

Put required dependencies in struct fields
and mark each required field inline with `// required`
when direct initialization is the right construction path.

```go
type Runner struct {
	Log  *silog.Logger // required
	Repo Repository    // required
}
```

Do not add a constructor
only to copy those fields into a struct.

## Errors

Wrap errors with context for the immediate sub-operation.
Do not use `failed to`, `error doing`,
or the current function name as error context.

```go
data, err := os.ReadFile(path)
if err != nil {
	return fmt.Errorf("read %q: %w", path, err)
}
```

Use `%q` for variable strings in errors
so empty strings and whitespace are visible.

Never use string matching
to detect a specific error type or condition.

## Control Flow

Use early returns for invalid or failure states.
Converge successful branches before shared work.
Model mutually exclusive behavior as a named mode
instead of a combination of booleans.

Keep mutation close to the operation that depends on it.
A longer visible sequence is better than a compressed expression
when the sequence makes state transitions easier to verify.

## Variables

Limit variable scope to the smallest useful block.
Declare variables close to first use.
Inline variables that are used only once
unless the expression is complex enough
that a name improves readability.

Initialize an empty slice with `var`:

```go
var items []Item
```

Use `make` for slices only when specifying length or capacity.

## Logging

Use `internal/silog.Logger`.
In tests, use `silogtest.New(t)` by default
so log output is attached to the test.
Use `silog.Nop()` only when the test needs to suppress logging entirely.
When asserting log output, capture logs with `silog.New(&buffer, nil)`.

## Imports

Avoid import aliases unless Go requires them
or local precedent already uses the alias.
Use a named import only for a real conflict,
such as two imported packages with the same package name.
The current file's package name is not a conflict
because code inside the package does not reference that package name.

## Exits

Do not call `log.Fatal`, `os.Exit`,
or similar hard exits outside `main`.
Return errors and let the caller decide how to report them.
