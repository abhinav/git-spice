# Contributing

We welcome contributes to the project,
but please discuss features or significant changes
in an issue before starting work on them.

## Tools

The following tools are needed to work on this project:

- [Go](https://go.dev/):
  The project is written in Go, so you need the Go compiler.
- [Changie](https://changie.dev/):
  We use Changie to manage the changelog.
  You'll need this if you make user-facing changes.
- [stitchmd](https://github.com/abhinav/stitchmd):
  We use stitchmd to generate the README from files inside the doc/ directory.
  You'll need this to edit the README.

## Making contributions

Follow the usual GitHub contribution process for making changes
with the following notes:

- Add changelog entries for user-facing changes with `changie new`.
- If you edit documentation in doc/, run `make README.md` to update the README.
  This requires stitchmd to be installed.
- All commits must include meaningful commit messages.
- Test new features and bug fixes.
  If it doesn't have a test, it's not fixed.
- Verify tests pass before submitting a pull request.

### Stacking changes

Unfortunately, it's not possible to submit a stack of pull requests
to a repository that you do not have write access to.
To work around this, we advise the following workflow
to stack changes with git-spice for a contribution:

1. Set your fork as the upstream remote for git-spice.

    ```bash
    gh repo fork --remote fork
    gs repo init --remote fork
    ```

2. After preparing your stack of branches, submit them to your fork.

    ```bash
    gs stack submit
    ```

3. Create a pull request to the upstream repository with the top branch
   of your stack.

## Testing

We use standard Go testing.

```sh
go test ./...
```

Use `make` to get a coverage report:

```sh
make cover
```

### Test scripts

Tests for the project make heavy use of the
[testscript package](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript).
Tests scripts are stored inside the testdata/script directory.
Read more about the test script format in the documentation of the package.

## Releasing a new version

(For maintainers only.)

To release a new version, take the following steps:

1. Trigger the [Prepare release workflow](https://github.com/abhinav/git-spice/actions/workflows/prepare-release.yml).
   This will create a pull request with the changelog entries for the release.
2. Merge the pull request created by the workflow.
   Feel free to edit it before merging if needed.
3. Once the pull request has merged, trigger the
   [Publishh release workflow](https://github.com/abhinav/git-spice/actions/workflows/publish-release.yml).
