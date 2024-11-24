# Test scripts

This directory contains test scripts
that verify the behavior of git-spice end-to-end.

The test script language is defined at [go-internal/testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript),
but in short:

- Commands are specified in the form, `name arg1 arg2 ...`,
  one per line.
- Comments begin with `#`.
- Strings are quoted with single quote only.
- Supporting files are specified below the script
  using `-- path/to/file --` syntax (see [txtar](https://pkg.go.dev/github.com/rogpeppe/go-internal/txtar))

## Testing external systems

For most external dependencies, we use fake implementations
controlled by environment variables or custom commands.
Some of these are listed in the [Command Reference](#command-reference).

### Secrets storage

The `secrettest` package provides a fake secrets stash
that can be used by gs commands to save and load secrets in a test script.
This is installed in all test scripts by default and requires no setup.

### Opening external URLs

Some commands open URLs in the browser.
By default, these operations no-op in test scripts.
To see what _would_ have been opened,
set `BROWSER_RECORDER_FILE` to a file path:

```
env BROWSER_RECORDER_FILE=$WORK/browser.log
```

This will record URLs that would have been opened in a browser,
one per line, to the specified file.

Afterwards, compare these against the test's golden copy:

```
cmpenv $WORK/browser.log $WORK/browser.log.golden
```

### Interactive prompts

Many commands in git-spice prompt the user for input.
To test these, we have two options:

- (**Deprecated**) `with-term`:
  This runs the command with an in-memory terminal emulator
  and a simple scripting language to control its behavior.
  Details are explained below in the command reference.
  Do not use this for new tests.
- (**Recommended**) `RobotView`:
  Replaces git-spice's concept of an interactive terminal
  with a robot that fills each prompt with a pre-defined answer.
  Answers are read from a fixture file.
  The rest of this section explains this further.

To use `RobotView`, write a fixture file with the following format
inside the test script with a file name ending in `.golden`:

```
===
> comment
"JSON-encoded answer"
===
> another comment
"another JSON-encoded answer"
```

The comment is optional when you first write it,
but the JSON-encoded answer is required.
These answers will be fed to the corresponding terminal widget in-order.

Before invoking `gs` in the test script,
set the `ROBOT_INPUT` environment variable to the fixture file.
Optionally, also set `ROBOT_OUTPUT` to another file
without the `.golden` suffix.

```
env ROBOT_INPUT=$WORK/robot.golden ROBOT_OUTPUT=$WORK/robot
```

When git-spice runs, it will read and answer prompts from the fixture file.
For each prompt, it will render the prompt and its input to the output file
(if set) in the same format as the fixture file.

After the test, you can compare the output file with the golden file:

```
cmpenv $WORK/robot $WORK/robot.golden
```

Use `-update` to automatically update the golden file when you first write it.

## Command Reference

Besides the base functionality of testscript,
we instrument the engine with the following additional commands:

### as

```
as 'User Name <user@example.com>'
```

Sets the author and committer of the commits that follow.

### at

```
at <YYYY-MM-DD HH:MM:SS>
```

Sets the timestamp of the commits that follow.

### cmpenvJSON

```
cmpenvJSON <want> <got>
```

Compares the contents of the two files as JSON,
replacing environment variables in the forms `${VAR}` and `$VAR`
with their values at the time of the comparison.

### git

```
[!] git <command> <args...>
```

Runs the real `git` command.

### gs

```
gs <command> <args...>
```

Runs git-spice itself.
This is built from the current source code
and has a testing-only Forge registered against it.
See [shamhub](#shamhub) for more details.

### mockedit

```
mockedit <file>
```

This is a fake text editor.
Its behavior can be controlled by setting the following environment variables:

- `MOCKEDIT_RECORD`:
  Path to a file where the original content will be saved.
- `MOCKEDIT_GIVE`:
  Path to a file that will be returned as the edited content.

If neither of these is set, the editor will fail.

### shamhub

```
shamhub <command> <args...>
```

ShamHub is a fake code forge with a REST API similar to GitHub.
`gs` binaries in test scripts are instrumented with this.
The shamhub command is used to control the behavior of the forge.

The following subcommands are available:

#### shamhub init

```
shamhub init
```

This must be run before any other shamhub commands.
It spins up a ShamHub server and
configures environment variables to be able to connect to it.

It makes two environment variables available:

- `SHAMHUB_URL`: The URL of the ShamHub server.
- `SHAMHUB_API_URL`: The URL of the ShamHub API.

#### shamhub new

```
shamhub new <remote> <owner/repo>
```

Creates a new repository on ShamHub with the given `<owner/repo>`,
and adds it as a remote to the local repository under the name `<remote>`.

#### shamhub clone

```
shamhub clone <owner/repo> <dir>
```

Clones a repository from ShamHub to the given directory.

#### shamhub merge

```
shamhub merge <owner/repo> <num>
```

Merges Change Request `<num>` made in the given repository
into its default branch.

### shamhub reject

```
shamhub reject <owner/repo> <num>
```

Rejects Change Request `<num>` made in the given repository.
Closes the CR without merging.

#### shamhub dump

```
shamhub dump changes
shamhub dump change <num>
shamhub dump comments
```

Dumps information about all changes, a single change, or all comments
to stdout.

#### shamhub register

```
shamhub register <username>
```

Registers a new user on ShamHub. No password is required.
To log in as the user, set `SHAMHUB_USERNAME` to the username
and run `gs auth login`.

### true

```
true <args...>
```

Exits with status 0 regardless of parameters or input.

### with-term

**Deprecated**: Do not use for new tests.

```
with-term [options] <script> -- <command> <args...>
```

Runs the given command inside a terminal emulator.
The `<script>` controls the terminal's behavior.
See the internal/termtest package for more details.

## Conditions

In addition to testscript's standard conditions,
the following are available:

### git:VERSION

```
[git:VERSION]
```

Evaluates to true only if the current version of Git
is at least the specified version.

VERSION must be a valid version string, such as `2.30`.
