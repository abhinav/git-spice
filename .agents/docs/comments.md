# Comments

Use this guide when adding, removing, or revising code comments,
symbol documentation, field comments, or package documentation.

## Choose The Reader

Documentation is for callers, importers, and package users.
It explains what a symbol promises, how to use it, and which constraints matter
at the boundary.

Implementation comments are for maintainers
who need to change the code.
They explain why code is shaped this way,
which invariant must hold, or what would break if the code were simplified.

Do not make callers read implementation comments
to learn a public contract.
Do not make maintainers infer hidden implementation constraints
from public documentation alone.

## What To Document

Document a named concept when the name and type do not fully explain:

- what the concept represents
- valid values or units
- ownership or lifetime
- side effects or ordering requirements
- error meanings
- external systems, protocols, file formats, or domain concepts

This applies to packages, exported symbols, request structs, state objects,
modes, interfaces, and fields.

## Field Comments

Comment struct fields when the field's meaning, source, valid values,
or caller obligation is not obvious from the name and type.

Use inline `// required` on required fields
when direct struct initialization is expected
or required-field checking should enforce initialization.

```go
type MoveRequest struct {
	Branch string // required
	Onto   string // required

	// ContinueCommand resumes this operation
	// after an interrupted rebase.
	ContinueCommand []string // required
}
```

## Implementation Comments

Use comments to reduce context a maintainer must reconstruct.
Good comments explain:

- why a non-obvious decision was made
- which invariant must be preserved
- which external behavior or compatibility rule matters
- what stage of a long operation the reader is entering
- what hidden state the next operations depend on

```go
// Flush queued writes before detaching transport state.
if conn.hasBufferedWrites() {
	conn.flush()
}
conn.stopWritePump()
```

## What To Avoid

Do not keep comments that only repeat the code.

```go
// Bad: repeats the operation.
count++ // increment count
```

Do not record chat history, discarded proposals, or implementation archaeology
unless that history is necessary to safely change the code.

If a comment cannot state the relevant invariant, boundary,
or reader obligation clearly,
reconsider the code shape before adding more prose.

## Formatting

Use `//` comments.
Do not use block comments.

Use full sentences for standalone comments.
Use sentence fragments for inline comments.
Use semantic line breaks for multi-line comments.
