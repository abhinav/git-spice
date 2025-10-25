---
name: test-script
description: Use when writing or modifying test scripts for git-spice commands. Test scripts are .txt files in testdata/script/. These verify end-to-end behavior of git-spice workflows. Use this skill when creating new test scripts, updating existing ones, or when asked to test complex git-spice workflows involving branch operations, forge interactions, or interactive prompts.
---

# Test Scripts

## Overview

Write test scripts for git-spice commands as `.txt` files.
Test scripts verify end-to-end behavior and are stored in `testdata/script/`.

## When to use this skill

Use this skill when:

- Writing new test scripts in testdata/script/
- Modifying existing .txt test scripts
- Testing complex git-spice workflows (branch operations, stack management, forge interactions)
- Verifying interactive prompts and user input scenarios

## Writing a test script

Test scripts are plain text files with a series of commands.
Most test scripts follow this general structure:

```
# Brief description of what the test does.
# Wrap comments at 80 characters.

as 'Test <test@example.com>'
at '2025-10-18T21:28:29Z'

# Setup
cd repo
git init
git commit --allow-empty -m 'Initial commit'
gs repo init

# (Optional) ShamHub setup for forge interactions
shamhub init
shamhub register alice
shamhub new origin alice/example.git
git push origin main

# Login as user
env SHAMHUB_USERNAME=alice
gs auth login

# ...

some command
cmp stdout $WORK/golden/output.txt

-- repo/file.txt --
file inside repo
-- repo/path/to/another-file.txt --
another file inside repo
-- extra/supporting-file.txt --
supporting file outside repo.
may be copied into repo during test.
-- golden/output.txt --
expected output for comparison
```

Notes:

- **Deterministic Git hashes**

    To get Git SHAs that are consistent across test runs,
    always freeze time and author at the start of the script
    using the `at` and `as` commands.

    ```
    as 'Test <test@example.com>'
    at '2025-10-18T21:28:29Z'
    ```

    The `at` should always use the current time
    at the moment of writing the test script.

- **Repository initialization**

    Initialize the Git repository and git-spice within it:

    ```
    cd repo
    git init
    git commit --allow-empty -m 'Initial commit'
    gs repo init
    ```

- **ShamHub setup**

    For tests involving forge interactions,
    we use a simulation server called ShamHub.
    To set it up, use the `shamhub` commands:

    ```
    shamhub init
    shamhub register alice
    shamhub new origin alice/example.git
    git push origin main

    env SHAMHUB_USERNAME=alice
    gs auth login
    ```

Following setup, use git-spice via `gs` commands to perform actions.

## Test script style

- **Naming**

    Follow the convention: `<command>_<scenario>.txt`
    Where `<command>` is the git-spice command being tested in `snake_case`,
    and `<scenario>` describes the specific test case.
    Examples:

    - `repo_sync_conflict.txt`
    - `stack_submit_with_labels.txt`

    If the test script is a regression test for a specific issue,
    include the issue number in the name:

    - `issue123_stack_rebase_does_not_do_a_thing.txt`

- **Comments**

    Use comments (`#`) to explain the purpose of each step.
    Wrap comments at 80 characters for readability.

    If the test script is a regression test for a specific issue,
    include a comment with the issue link at the top.

## Common tasks

### Creating branches

```
gs branch create <branch-name> -m <commit-msg>
```

This will create a new branch
and commit all staged changes to it
with the given commit message.

See `gs branch create --help` for more options.

Never use manual `git checkout -b`
unless specifically testing a non-git-spice branch creation scenario.

### Making commits

```
gs commit create -m <commit-msg>
```

This will commit all staged changes
to the current branch with the given commit message.

See `gs commit create --help` for more options.

### Supporting files

Files needed by test scripts are defined
at the end of the script using `-- path/to/file --` syntax.

For each such section,
everything after the `-- path/to/file --` line
until the next `-- path/to/another/file --` line, or the end of the file,
is treated as the contents of that file.

```
-- repo/feature.txt --
Initial content of feature.txt
-- extra/feature-new.txt --
Updated content of feature.txt
-- golden/output.txt --
Expected output of the command
```

### Using golden files

If the expected output of any command is stored in a supporting file,
put it inside a `golden/` subdirectory in the test script,
and refer to it using `$WORK/golden/filename`.

**Tip**:
For outputs that we do not control (e.g. git commit hashes),
save time by putting placeholder values in the golden files,
then run the test script with the `--update` flag to auto-update them.
(This only works for `cmp` comparisons, not `cmpenv` or `cmpenvJSON`.)

### Verifying git state

- View git graph:

    ```
    git graph --branches
    cmp stdout $WORK/golden/graph.txt
    ```

- View git status:

    ```
    git status --porcelain
    cmp stdout $WORK/golden/status.txt
    ```

- Verify git-spice branch relationships:

    ```
    gs ls
    cmp stderr $WORK/golden/ls.txt
    ```

- Verify git-spice branch and commit details:

    ```
    gs ll
    cmp stderr $WORK/golden/ll.txt
    ```

### Verifying command output

- Matching entire stdout

    ```
    run some command
    cmp stdout $WORK/golden/output.txt
    ```

- Matching stdout partially

    ```
    run some command
    stdout 'expected output substring'
    ```

- Matching entire stderr

    ```
    run some command
    cmp stderr $WORK/golden/error.txt
    ```

- Matching stderr partially

    ```
    run some command
    stderr 'expected error message substring'
    ```

- Comparing with environment variable substitutions

    ```
    run some command
    cmpenv stdout $WORK/golden/output.txt
    cmpenv stderr $WORK/golden/error.txt
    ```

    (`$FOO` placeholders in the golden file
    will be replaced with their actual values from the test environment.)

- Comparing JSON output with environment variable substitutions

    ```
    run some command --json
    cmpenvJSON stdout $WORK/golden/output.json
    ```

    (`$FOO` placeholders in the golden JSON file
    will be replaced with their actual values from the test environment.)

### Testing Interactive Prompts

For commands with interactive prompts,

- Declare a golden fixture file with pre-defined answers
  for each expected prompt.
  These are JSON-encoded answers separated by `===` lines,
  with `>` comments that contain the rendered prompt text.

  ```
  -- robot.golden --
  > Enter your name:
  "Alice"
  ===
  > Choose an option:
  >  1) Option A
  >  2) Option B
  2
  ```

- Before invoking the interactive command,
  set the `ROBOT_INPUT` environment variable
  to point to the fixture file,
  and `ROBOT_OUTPUT` to capture rendered prompts.

  ```
  env ROBOT_INPUT=$WORK/robot.golden ROBOT_OUTPUT=$WORK/robot.actual
  ```

- Run the command that prompts for input.

  ```
  gs some interactive command
  ```

- After the command completes,
  compare the captured output file
  with the golden fixture file.

  ```
  cmp $WORK/robot.actual $WORK/robot.golden
  ```

  (Use `cmpenv` if environment variable substitutions are needed.)

### Testing opening browser URLs

For commands that open browser URLs,
set `BROWSER_RECORDER_FILE` to a file path
that will capture the URLs that were requested to be opened.

```
env BROWSER_RECORDER_FILE=$WORK/browser.log
gs branch submit --web
```

Verify the recorded URLs against the golden copy:

```
cmpenv $WORK/browser.log $WORK/golden/browser.log
```

### Common mistakes in test scripts

- **Mistake**:
  Trying to use shell redirections (`>`, `>>`, `2>`) or pipes (`|`).
  **Problem**:
  Test scripts do not support shell operations.
  **Fix**:
  Use `cmp stdout`, `cmp stderr`, `stdout`, and `stderr`.
  There's no way to redirect output in test scripts.
- **Mistake**:
  Using `cat`, `ls`, or similar commands to check file existence.
  **Fix**:
  Use `exists filename` command for checking file existence.
  Use `cmp have_file want_file` to compare file contents,
  where `want_file` is defined in the golden section.

## Running test scripts

- **Run a specific test script:**

    ```bash
    mise run test:script --run $name
    ```

    Where `$name` is the script name without `.txt` extension.
    For example, to run `testdata/script/repo_sync_conflict.txt`:

    ```bash
    mise run test:script --run repo_sync_conflict
    ```

- **Auto-update golden files:**

    If a test script fails because the expected output
    has changed intentionally,
    use the `--update` flag to update golden files:

    ```bash
    mise run test:script --run $name --update
    ```

    **Note**:
    This only works for `cmp` comparisons.
    `cmpenv` and `cmpenvJSON` do not support `--update` mode.

## ShamHub

ShamHub is a simulated forge server (e.g., GitHub/GitLab)
used for testing git-spice's forge interactions.

Outside of using git-spice (`gs`) commands,
ShamHub interactions are available through the `shamhub` command
defined at internal/forge/shamhub/cli.go.

Common `shamhub` commands:

- `shamhub init`:
  Initialize ShamHub server, set environment variables.
- `shamhub register <username>`:
  Register users on ShamHub.
- `shamhub new <remote> <owner/repo>`:
  Create a new ShamHub repository
  and add as remote in the current Git repository.
- `shamhub fork <owner/repo> <fork-owner>`:
  Create a fork of a ShamHub repository under a different user.
- `shamhub clone <owner/repo> <dir>`:
  Clone an existing ShamHub repository into the given directory.
- `shamhub merge [-prune] [-squash] <owner/repo> <pr>`:
  Merge a Shamhub Change Request (Pull Request/Merge Request).
  Use `-prune` to delete the source branch after merging.
  Use `-squash` to squash commits when merging.
- `shamhub reject <owner/repo> <pr>`:
  Reject a ShamHub Change Request and close it.
- `shamhub dump changes/comments`:
  Dump ShamHub state to stdout for verification.
  (Use `cmp`, `cmpenv`, and `cmpenvJSON` to verify output.)

## Reference

For detailed command reference,
see `testdata/script/README.md` in the repository.
