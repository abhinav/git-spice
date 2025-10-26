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

# Run a specific test (e.g. `auth_detect_forge.txt`)
mise run test:script --run auth_detect_forge

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

## Backporting changes

(For maintainers only.)

To backport a change to a previous release branch,
take the following steps:

1. Determine the series name.
   This is the combination of the major and minor version numbers,
   e.g. for version `v0.5.2`, the series name is `0.5`.

   ```bash
   SERIES=0.5
   ```

2. Ensure that a release series branch exists.
   This is normally named `release/vX.Y.x`,
   where `X.Y` is the series name determined above.

    ```bash
    BRANCH="release/v${SERIES}.x"
    if git branch -r --list "origin/${BRANCH}" | grep -q .; then
        echo "Release branch ${BRANCH} alrady exists."
    else
        echo "Release branch ${BRANCH} does not exist."
        LATEST_TAG=$(git tag --list "v${SERIES}.*" --sort=-v:refname | head -n 1)
        echo "Creating branch ${BRANCH} from latest tag ${LATEST_TAG}."
        echo "Press Enter to continue."
        read
        git branch "${BRANCH}" "${LATEST_TAG}"
        git push origin "${BRANCH}"
    fi
    ```

3. Create a backport branch from the release branch.

    ```bash
    BACKPORT_BRANCH="backport-${SERIES}-$(date +%Y%m%d%H%M%S)"
    git checkout -b "${BACKPORT_BRANCH}" "origin/${BRANCH}"
    ```

4. Cherry-pick the desired commits to backport.

    ```bash
    git cherry-pick <commit-hash-1> <commit-hash-2> ...
    ```

5. Prepare the changelog for the backport.

   ```bash
   changie batch patch && changie merge
   git add .changes CHANGELOG.md
   NEW_VERSION=$(changie latest)
   git commit -m "Release ${NEW_VERSION}"
   ```

5. Submit a pull request from the backport branch
   against the release branch, e.g.

   ```bash
   git push -u origin "${BACKPORT_BRANCH}"
   gh pr create \
     --base "${BRANCH}" \
     --head "${BACKPORT_BRANCH}" \
     --title "Release ${NEW_VERSION}"
   ```

6. Once the pull request is merged,
   trigger the
   [Publish release workflow](https://github.com/abhinav/git-spice/actions/workflows/publish-release.yml)
   for the release branch to create a new release.

7. Merge the release branch back to main.

    ```bash
    git checkout main
    git merge "origin/${BRANCH}"
    git push origin main
    ```
