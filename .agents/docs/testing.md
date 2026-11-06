# Testing

Use this guide when adding, changing, or reviewing tests.

## Regression Tests

A bug fix requires a regression test
that fails before the fix
and passes after the fix.

Prefer a unit test when the behavior can be exercised directly.
Use a test script when the behavior is a command workflow, shell contract,
or end-to-end user-visible interaction.

When Git, forge, or process behavior matters,
prefer a real-boundary probe or fixture-backed test
over a mock that only proves the implementation called a helper.

## Useful Tests

A useful test protects an observable promise:
what callers can rely on, what users can see,
what state transition must happen, or what error means.

Avoid tests that only detect private implementation shape.
If a different correct implementation would fail the test,
the test may be too coupled to mechanics.

Keep the scenario visible in the test body.
Helpers are useful when they name real setup operations.
Helpers that only hide required fields, mock construction,
or assertions make tests harder to inspect.

## Unit Tests

Use `t.Context()` instead of `context.Background()`.
Use `testify/assert` and `testify/require`.
Use `silogtest.New(t)` by default
so log output is attached to the test.
Use `silog.Nop()` only when the test needs to suppress logging entirely.

When asserting log output, capture logs with a buffer:

```go
var logBuffer bytes.Buffer
log := silog.New(&logBuffer, nil)
```

Use `defer` for cleanup.
Inside deferred cleanup, use `assert.NoError`
so cleanup can continue.

## Test Organization

Use table tests for simple scenarios
that share setup and teardown.
Use subtests when scenarios need different setup, different teardown,
or enough local detail that a table would hide the case.

Do not use test tables with `func` fields.

Name tests with GoCase symbol names:

```text
Test{Name}
Test{Type}_{Method}
Test{Name}_{scenario}
Test{Type}_{Method}_{scenario}
```

The scenario suffix starts with a lower-case letter.

Use GoCase subtest names, such as `AlreadyRestacked` or `NeedsRestack`.

```go
t.Run("AlreadyRestacked", func(t *testing.T) {
	// ...
})
```

Place helper functions below all test functions.

## Test Scripts

Test scripts live in `testdata/script`.
They use the txtar-based script format described in
`testdata/script/README.md`.
Read `.agents/skills/test-script/SKILL.md`
before adding or changing a test script.

Name scripts with the `<command>_<scenario>.txt` convention.
Reserve `issueNNN_...` prefixes only for bug-fix regression scripts
that are specifically tied to that issue.
Do not use `issueNNN_...` names for feature scripts.

Run a single script during development:

```bash
mise run test:script --run $name
```

Update a script's golden output only after verifying
the new behavior is intended:

```bash
mise run test:script --run $name --update
```

## Mocks

Use `gomock` only when a real dependency would make the test slower,
less deterministic, or unable to isolate the behavior under test.

Create the controller at the start of the test:

```go
mockCtrl := gomock.NewController(t)
```

Do not call `defer mockCtrl.Finish()`;
the controller handles cleanup automatically.

Declare mocks next to first use, especially when a test uses multiple mocks.
Inline mocks with no expectations
unless the mock value is used more than once.

Format expectations in multi-line style:

```go
mockService.EXPECT().
	MethodName(gomock.Any(), "param").
	Return(expectedResult)
```

Generate mocks with `//go:generate mockgen`.
Use `mocks_test.go` or `mock_foo_test.go`
unless the destination package is a test-only package
with a name ending in `test`.
In test-only packages, use `mocks.go` or `mock_foo.go`.

Example:

```go
//go:generate mockgen -destination mocks_test.go -package track -typed . GitRepository,Service
```
