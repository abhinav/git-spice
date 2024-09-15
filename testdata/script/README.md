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

## Commands

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
