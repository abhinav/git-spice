# Development workflow

## Quick reference

| Step                     | Command                                         |
|--------------------------|-------------------------------------------------|
| Verify code compiles     | Use `mcp__gopls__go_diagnostics` (if available) |
| Build project            | `mise run build`                                |
| Update generated files   | `mise run generate`                             |
| Run linters              | `mise run lint`                                 |
| Format code              | `mise run fmt`                                  |
| Run all tests            | `mise run test` (use sparingly; slow)           |
| Run specific unit tests  | `go test ./path/to/package -run TestRegex`      |
| Run all test scripts     | `mise run test:script` (use sparingly; slow)    |
| Run specific test script | `mise run test:script --run $name`              |
| Update test script       | `mise run test:script --run $name --update`     |
| Add changelog entry      | `changie new --kind $kind --body $body`         |

## Overview

Features and bug fixes must always follow these steps
unless explicitly stated otherwise:

- **Core development loop**:
  Use the following cycle during development:

  1. Write tests

     Test-driven development is preferred.
     If not possible, discuss with the user before writing code.

     - For features, propose a series of test scripts (see Skill(test-script))
       that demonstrate the desired behavior before writing code.
     - For bug fixes, write a regression test first:
       prefer a unit test, but a test script is acceptable if necessary.

     In both cases, verify test failure before writing code.

  2. Write code

      Use `mcp__gopls__go_diagnostics` to verify code compiles,
      fixing issues as they arise.
      It is acceptable to have temporary compilation errors
      during refactoring if planned changes will resolve them.

  3. Get tests passing

      Run only the new or modified tests during development to save time.

- **Finishing up**:
  When the user indicates that the change is ready to be finalized
  (ask if unsure), perform these steps:

    1. Format

        Run the following command and accept its changes:

        ```bash
        mise run fmt
        ```

    2. Lint

        Run the following command and fix any issues reported:

        ```bash
        mise run lint
        ```

    3. Build

        Verify the project builds successfully:

        ```bash
        mise run build
        ```

    4. Generate

        If the change adds or modifies CLI commands, flags, or
        configuration options (e.g., adding a new forge),
        regenerate documentation and other generated files:

        ```bash
        mise run generate
        ```

        This updates `doc/includes/cli-reference.md` and other generated files.

    5. Changelog

        If the change is user-facing, that is,
        it adds or modifies functionality visible to end users,
        add a changelog entry.
        Internal changes, refactors, and test-only changes
        do not require changelog entries.
        Ask if unsure.

        ```bash
        changie new --kind $kind --body $body
        ```

    6. Commit

        Create a commit with a descriptive message.
        Follow user-specified guidelines for commit messages if provided.

## Testing

This project has two kinds of tests:

- **Unit tests**:
  Written as regular Go tests using the `testing` package.
  These are fast to run and should be used for most testing.
- **Test scripts**:
  Written inside testdata/script/ directory as a series of `*.txt` files
  in a shell-like syntax (note: NOT actual shell scripts).
  The syntax for these is described in testdata/script/README.md.

### Unit tests

#### Testing best practices

Always follow these best practices
when writing or modifying unit tests:

- **Context**:
  Use `t.Context()` instead of `context.Background()`.
- **Assertions**:
  Use `testify/assert` and `testify/require` for assertions.
- **Logging**:
  - Use `silog.Nop()` when log output is not being tested.
  - When the log output is being tested (e.g., verifying log messages),
    use `silog.New(&logBuffer, nil)` with a `bytes.Buffer` to capture log output.
    Verify important log messages with
    `assert.Contains(t, logBuffer.String(), "message")`.
- **Cleanup**:
  When tests create resources (branches, files, etc.),
  use `defer` with proper error handling to clean up.
  In deferred cleanup functions, use `assert.NoError`
  rather than `require.NoError` to avoid stopping cleanup.

#### Test organization

- Use table tests for simple scenarios
  where all test cases share the same setup and teardown logic.
- Use subtests instead of table tests
  when scenarios are complex and require different setup/teardown logic.
- Use separate test functions for simple, independent scenarios.
- Group related test cases under a common parent test function
  using subtests.
- NEVER use test tables with `func` fields
  (i.e., anonymous functions inside test case structs).

#### Test naming

- Use `Test{Name}` for tests for specific symbols.
  `{Name}` always starts with a capital letter (e.g. `TestFoo`).
- Use `Test{Type}_{Method}` for tests for methods.
  `{Type}` and `{Method}` start with capital letters (e.g. `TestBar_Baz`).
- Use `Test{Name}_{scenario}` or `Test{Name}_{Method}_{scenario}`
  for tests for specific scenarios under a symbol or method.
  `{scenario}` starts with a lowercase letter
  (e.g. `TestFoo_invalidInput` or `TestBar_Baz_edgeCase`).
- Use GoCase for subtest names
  (e.g., "AlreadyRestacked", "NeedsRestack").

#### Test function ordering

- Order test functions with related tests together.
- Add new test functions below existing test functions
- Helper functions must always go at the bottom of the file
  below all test functions.

### Mocks

Unit tests that require interfaces to be mocked
make use of the `gomock` library
and the accompanying `mockgen` tool.

#### Generating mocks

If a test requires a mock of an interface,
add a `//go:generate mockgen` comment as follows:

```go
//go:generate mockgen -destination=<destination> -package=<package-name> -write_package_comment=false -typed=true <import-path> <Iface1,Iface2,...>
```

Explanation:

- `<destination>`:
  Path to the generated mock file.
  Use a name ending in `*_test.go`, e.g. `mocks_test.go`.
  This ensures the mock is only included in test builds.
  - Exception: The package is a test utility package
    (name ending in `test`, e.g. `footest`).
- `<package-name>`:
  Name of the package for the generated mock file.
  Matches package name used in source files in the same directory,
  or the name of the directory.
- `<import-path>`:
  Full import path of the package containing the interfaces to be mocked.
  This will usually be `.` for interfaces defined in the same package
  as the test using the mock.
- `<Iface1,Iface2,...>`:
  Comma-separated list of interface names to be mocked.

After adding or modifying a `//go:generate mockgen` comment,
run `go generate` in the package directory
or run `mise run generate` to regenerate all mocks in the codebase.

#### Using mocks

At the start of a test function that uses mocks, set up a `gomock.Controller`:

```go
mockCtrl := gomock.NewController(t)
// DO NOT use `defer ctrl.Finish()`; this is handled automatically.
```

Instantiate mocks using the generated mock types
next to the first use of the mock:

```go
mockService := NewMockService(mockCtrl)
```


Format mock expectations using multi-line style for readability:

```go
mockService.EXPECT().
    MethodName(gomock.Any(), "param").
    Return(expectedResult)
```

#### Common mistakes with mocks

- **Mistake**: Declaring all mocks at the start of the test function.
  **Fix**: Declare mocks next to their first use.

  ```go
  // Bad: mocks are declared at start of function
  func TestSomething(t *testing.T) {
      mockCtrl := gomock.NewController(t)
      mockService := NewMockService(mockCtrl)
      mockRepo := NewMockRepo(mockCtrl)

      // ...

      mockService.EXPECT().
          DoSomething(gomock.Any()).
          Return(nil)
      doStuff(mockService)

      // ...

      mockRepo.EXPECT().
          GetData("id").
          Return(&Data{}, nil)
      data, err := fetchData(mockRepo, "id")
  }

  // Good: mocks are declared next to first use
  func TestSomething(t *testing.T) {
      mockCtrl := gomock.NewController(t)

      // ...

      mockService := NewMockService(mockCtrl)
      mockService.EXPECT().
          DoSomething(gomock.Any()).
          Return(nil)
      doStuff(mockService)

      // ...
      mockRepo := NewMockRepo(mockCtrl)
      mockRepo.EXPECT().
          GetData("id").
          Return(&Data{}, nil)
      data, err := fetchData(mockRepo, "id")
  }
  ```

- **Mistake**: Creating mock variables for mocks without expectations.
  **Fix**: Inline mocks without expectations directly where they are used.
  **Exception**: If the mock is used multiple times in the test, don't inline.

  ```go
  // Bad: mock variable created for mock without expectations
  func TestSomething(t *testing.T) {
      mockCtrl := gomock.NewController(t)
      mockService := NewMockService(mockCtrl)
      doStuff(mockService)
  }

  // Good: mock inlined directly where used
  func TestSomething(t *testing.T) {
      mockCtrl := gomock.NewController(t)
      doStuff(NewMockService(mockCtrl))
  }

  // Exception: mock used multiple times
  func TestSomething(t *testing.T) {
      mockCtrl := gomock.NewController(t)
      mockService := NewMockService(mockCtrl)
      doStuff(mockService)
      verifyStuff(mockService)
  }
  ```

## Keeping the changelog

We use the `changie` tool to manage the changelog.
Unreleased changes are stored in the .changes/unreleased directory.

To add a changelog entry for a user-facing change, run:

```
changie new --kind $kind --body $body
```

Where:

- `$kind` is one of:
  - Added: a new feature
  - Changed: a change to existing functionality
  - Deprecated: a feature that is deprecated
  - Removed: a removed feature
  - Fixed: a bug fix
  - Security: a security fix
- `$body` is a description of the change in passive voice.
  - **IMPORTANT**: Describe the user-facing change,
    not the internal implementation detail.
    If there are no user-facing changes, do not add a changelog entry.
  - For component-specific changes,
    prefix the description with the component name,
    e.g., "submit: Add 'config.option' to enable feature".
  - Components can be command names (e.g., "repo sync")
    or command domains (e.g., "submit", "github").
  - See CHANGELOG.md for existing patterns.

To skip the changelog check for internal changes, refactors,
or test-only changes, include `[skip changelog]: <cause description>` in the PR description as a trailer.

# Code Quality

- Never introduce new third-party dependencies.

## Code style

- **Line length**

  Prefer a soft limit of 80 characters per line.
  This includes code, comments, and documentation.
  Aim to keep lines within this limit for readability.

  Never exceed 120 characters per line.

- **Package naming**

  Do not pluralize package names.
  Use singular nouns or compound names instead.

  ```
  // BAD: pluralized package names
  urls
  utils
  helpers

  // GOOD: singular or compound names
  forgeurl
  stringutil
  testhelper
  ```

- **Logical grouping with comments**

  Use descriptive section headers to group related operations

- **Reduce variable scope**

  Limit variable scope to the smallest possible block.
  Declare variables closest to their first use.

- **Minimize variable declarations**

  Do not create variables that are used only once.
  Inline such values directly where they are used.

  ```go
  // BAD: unnecessary variable declaration
  result := computeValue()
  process(result)

  // GOOD: inline value directly
  process(computeValue())
  ```

  ```go
  // BAD: unnecessary variable declaration
  handler := RequestHandler{
      Field: value,
  }
  handler.HandleRequest(request)

  // GOOD: inline value directly
  (&RequestHandler{
      Field: value,
  }).HandleRequest(request)
  ```

  Exceptions:

  - The variable is used multiple times.
  - The expression is already complex and inlining would reduce readability.

- **Initializing slices**

  To initialize an empty slice, always use `var` form:

  ```go
  // GOOD
  var items []Item
  ```

  Use `make` only for slices with a non-zero length or capacity:

  ```go
  // GOOD
  items := make([]Item, length)
  items := make([]Item, length, capacity)
  ```

- **String concatenation**

  For simple string concatenation, use `+`:

  ```go
  // GOOD
  fullName := firstName + " " + lastName

  // BAD
  fullName := fmt.Sprintf("%s %s", firstName, lastName)
  ```

## Code patterns

- **Immediately-invoked functions for short-lived defer**

  If a function needs a `defer` that is only relevant within a small scope,
  encapsulate that logic in an immediately-invoked function.

  ```go
  err := func() error {
      res, err := doSomething()
      if err != nil {
          return err
      }
      defer res.Close()

      // ...

      return nil
  }()
  if err != nil {
      return // ...
  }

  // ...
  ```

- **Error wrapping**

  - Always add context when propagating errors up the call stack.

    ```go
    // BAD: no context added
    data, err := fetchData()
    if err != nil {
        return err
    }


    // GOOD: context added
    data, err := fetchData()
    if err != nil {
        return fmt.Errorf("fetch data: %w", err)
    }
    ```

  - Add context for only the current sub-operation that failed.

    ```go
    // BAD: context for entire operation repeated
    func process(input string) error {
        data, err := fetchData(input)
        if err != nil {
            return fmt.Errorf("process input %q: fetch data: %w", input, err)
        }
        // ...
    }

    // GOOD: context only for failing sub-operation
    func process(input string) error {
        data, err := fetchData(input)
        if err != nil {
            return fmt.Errorf("fetch data: %w", err)
        }
        // ...
    }
    ```

    Rationale:
    The higher-level context will be added by the caller.

  - Do not add "failed to x", "error doing y", or similar phrases
    to wrapped error context.

    ```go
    // BAD: redundant failure indication
    if err != nil {
        return fmt.Errorf("failed to fetch data: %w", err)
    }

    // GOOD: no redundant failure indication
    if err != nil {
        return fmt.Errorf("fetch data: %w", err)
    }
    ```

    Rationale: The presence of an error already indicates failure.

## Comment style

- Use full sentences for standalone comments
  (i.e., comments that are not inline with code).

  ```go
  # BAD
  // initialize the user repository

  # GOOD
  // Initialize the user repository.
  ```

- Use sentence fragments for inline comments
  (i.e., comments that are on the same line as code).

  ```go
  # BAD
  value := computeValue() // This computes the value

  # GOOD
  value := computeValue() // compute the value
  ```

- Never add comments that merely repeat the code.

  ```go
  # BAD
  count := len(items) // get the length of items

  # GOOD
  count := len(items)
  ```

- Always use `//`-style comments.
  Never use `/* ... */`-style comments.

- Use semantic line breaks in comments.
  This requires breaking lines at natural grammatical boundaries
  (such as after complete sentences, clauses, or list items),
  while remaining within the 80-character limit.

# Documentation

Markdown documentation resides inside `doc/src`.
The layout and structure of the documentation
is described in `doc/mkdocs.yml`.

When documenting unreleased features, add the following placeholder:

```markdown
<!-- gs:version unreleased -->
```

This will be automatically updated to the correct version
when the feature is released.

## Documentation style

- Wrap lines at 80 characters.
- Always follow semantic line breaks in documentation.
  This requires breaking lines at natural grammatical boundaries
  (such as after complete sentences, clauses, or list items),
  while remaining within the 80-character limit.
