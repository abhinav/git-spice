# Contributing

We welcome contributes to the project,
but please discuss features or significant changes
in an issue before starting work on them.

## Setup

We use [mise](https://mise.jdx.dev) to manage tools dependencies
and development tasks for the project.

If you already have mise set up, `cd` into this directory
and you can begin working on the project.

If you don't have mise set up, you may:

- [Install and set it up](https://mise.jdx.dev/getting-started.html), or
- Use the bootstrap script at `tools/bin/mise` to open a shell session
  with mise and all tools installed.

    ```bash
    ./tools/bin/mise en
    ```

    This is a good option if you don't want to change your setup.
    It will keep mise and all its state entirely within the project directory.

### Tasks

See available tasks with `mise tasks` in the project directory.

Tasks you'll need to run regularly are:

```
# Build the project
mise run build

# Run linters
mise run lint

# Run all tests
mise run test

# Run a documentation server
mise run doc:serve
```

## Making contributions

Follow the usual GitHub contribution process for making changes:
fork and create a pull request.

Follow usual best practices for making changes:

- All commits must include meaningful commit messages.
- Test new features and bug fixes.
  If it does not have a test, the bug is not fixed.
- Verify tests pass before submitting a pull request.

More specific guidelines follow:

- For all *user-facing changes*, add a changelog entry.
  We use [Changie](https://changie.dev) for this.
  Run `mise run changie new` to add a changelog entry.

  If a change is not user-facing,
  add a note in the following format to the PR description:

  ```
  [skip changelog]: reason why no changelog entry is needed
  ```

- For *documentation website changes* (changes made to the doc/ directory),
  run `mise run doc:serve` (or just `mise run serve` in the doc/ directory)
  to preview changes locally before submitting a pull request.

- For *code changes*,
  ensure generated code is up-to-date (`mise run generate`),
  code is well-formatted (`mise run fmt`)
  and all lint checks pass (`mise run lint`).

### Stacking changes

Unfortunately, it's not possible to submit a stack of pull requests
to a repository that you do not have write access to.
To work around this, we advise the following workflow
to stack changes with git-spice for a contribution:

1. Set your fork as the upstream remote for git-spice.

    ```bash
    gh repo fork --remote --remote-name fork
    gs repo init --remote fork
    ```

2. After preparing your stack of branches, submit them to your fork.

    ```bash
    gs stack submit
    ```

3. Create a pull request to the upstream repository with the top branch
   of your stack.

## Style

### Code style

Guidelines from the following sources apply:

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

When these conflict, the maintainer's preference takes precedence.

### Documentation style

This section covers style guidelines for both
Markdown files and code comments.

- Paragraphs must employ [Semantic Line Breaks](https://sembr.org/).
  Do not write overly long sentences on single lines.
  Do not "re-flow" paragraphs to fit within N columns.

  ```
  Bad paragraph with sentences all in one line in the file. Text editors will not wrap this by default. Browsers will require horizontal scrolling when reviewing the raw text.

  Bad paragraph with sentence text re-flowed to fit within 80 columns. While
  this is more readable, it makes it more annoying to edit a single clause of
  the sentence during review. Diffs to a single clause in a sentence can reflow
  the entire paragraph.

  Good paragraph employing semantic line breaks.
  Each sentence is on its own line,
  or even across multiple lines if needed.
  Easy to read in raw code form
  and easy to edit singular clauses during review.
  ```

- Markdown must use `#`-style headers, not `=` or `-` style.

  ```
  Bad header
  ==========

  ## Good header
  ```

- In code, all exported symbols must be documented with `//`-style comments.

## Testing

Use mise to run tests:

```sh
mise run test
```

If you need to see test coverage, run:

```sh
mise run cover
```

### Test scripts

Tests for the project make heavy use of the go-internal/testscript package.
Read more about the test script language at [testscript package](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript).

Tests scripts are stored inside the testdata/script directory.
On top of the base functionality of testscript,
we add a bunch of custom commands and infrastructure to make testing easier.

Read more about these in the [test scripts README](testdata/script/README.md).

## Releasing a new version

(For maintainers only.)

To release a new version, take the following steps:

1. Trigger the [Prepare release workflow](https://github.com/abhinav/git-spice/actions/workflows/prepare-release.yml).
   This will create a pull request with the changelog entries for the release.
2. Merge the pull request created by the workflow.
   Feel free to edit it before merging if needed.
3. Once the pull request has merged, trigger the
   [Publishh release workflow](https://github.com/abhinav/git-spice/actions/workflows/publish-release.yml).
