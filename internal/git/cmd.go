// Package git provides access to the Git CLI with a Git library-like
// interface.
//
// All shell-to-Git interactions should be done through this package.
package git

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"iter"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// execer controls actual execution of Git commands.
// It provides a single place to hook into for testing.
type execer = xec.Execer

var _realExec = xec.DefaultExecer

type extraConfig struct {
	Editor string // core.editor

	MergeConflictStyle string // merge.conflictStyle

	AdviceMergeConflict *bool // advice.mergeConflict
}

// args builds the git -c flags for the configured values.
func (ec *extraConfig) args() []string {
	var args []string
	if ec.Editor != "" {
		args = append(args, "-c", "core.editor="+ec.Editor)
	}
	if ec.MergeConflictStyle != "" {
		args = append(args, "-c", "merge.conflictStyle="+ec.MergeConflictStyle)
	}
	if ec.AdviceMergeConflict != nil {
		args = append(args, "-c",
			fmt.Sprintf("advice.mergeConflict=%t", *ec.AdviceMergeConflict))
	}
	return args
}

// gitCmd is a package-local wrapper around [xec.Cmd]
// for Git CLI operations issued by this package.
//
// It centralizes Git-specific command policy in [newGitCmd]
// while preserving the fluent API that internal/git callers expect.
type gitCmd struct {
	cmd *xec.Cmd
}

// _readOnlyGitCmds is the set of git subcommands
// that do not require write access to the index.
// These commands receive GIT_OPTIONAL_LOCKS=0
// to avoid contending with concurrent writers.
var _readOnlyGitCmds = map[string]struct{}{
	"cat-file":     {},
	"config":       {},
	"diff":         {},
	"diff-files":   {},
	"diff-index":   {},
	"diff-tree":    {},
	"for-each-ref": {},
	"log":          {},
	"ls-files":     {},
	"ls-remote":    {},
	"ls-tree":      {},
	"merge-base":   {},
	"merge-tree":   {},
	"remote":       {},
	"rev-list":     {},
	"rev-parse":    {},
	"show":         {},
	"symbolic-ref": {},
	"var":          {},
	"worktree":     {},
}

// newGitCmd builds a new Git command for the given Git subcommand
// and arguments.
//
// If the logger is at Debug level or lower,
// stderr of the command will be written to the logger.
// Otherwise, it will be captured and surfaced in the error
// if the command fails.
//
// This allows for a nicer, less noisy UX for expected errors:
//
//   - if a Git command was expected to fail,
//     and the error is never logged,
//     its stderr output will not be shown to the user.
//   - if the error is logged,
//     the stderr output will be shown to the user.
//   - if the program is running in verbose mode,
//     the stderr output will always be shown to the user,
//     but it won't be duplicated in the error message.
func newGitCmd(
	ctx context.Context,
	log *silog.Logger,
	exec execer,
	subcmd string,
	args ...string,
) *gitCmd {
	prefix := "git"
	if subcmd != "" {
		prefix += " " + subcmd
	}

	argv := append([]string{subcmd}, args...)
	cmd := xec.Command(ctx, log, "git", argv...).
		WithExecer(exec).
		WithLogPrefix(prefix)

	if _, ok := _readOnlyGitCmds[subcmd]; ok {
		cmd.AppendEnv("GIT_OPTIONAL_LOCKS=0")
	}

	return &gitCmd{cmd: cmd}
}

// WithExtraConfig prepends transient git -c configuration
// to the wrapped command.
//
// This is the package-local mechanism for applying per-command Git settings
// such as core.editor or merge.conflictStyle.
func (c *gitCmd) WithExtraConfig(ec *extraConfig) *gitCmd {
	if ec == nil {
		return c
	}

	args := ec.args()
	if len(args) == 0 {
		return c
	}

	args = append(args, c.Args()...)
	return c.WithArgs(args...)
}

// Run runs the wrapped command, blocking until it completes.
func (c *gitCmd) Run() error {
	return c.cmd.Run()
}

// Start starts the wrapped command, returning immediately.
func (c *gitCmd) Start() error {
	return c.cmd.Start()
}

// Wait waits for a started command to complete.
func (c *gitCmd) Wait() error {
	return c.cmd.Wait()
}

// Output runs the wrapped command and returns its stdout.
func (c *gitCmd) Output() ([]byte, error) {
	return c.cmd.Output()
}

// OutputChomp runs the wrapped command
// and returns stdout with trailing whitespace removed.
func (c *gitCmd) OutputChomp() (string, error) {
	return c.cmd.OutputChomp()
}

// Scan runs the wrapped command
// and yields stdout tokens split by the provided function.
func (c *gitCmd) Scan(split bufio.SplitFunc) iter.Seq2[[]byte, error] {
	return c.cmd.Scan(split)
}

// Lines runs the wrapped command
// and yields stdout line by line.
func (c *gitCmd) Lines() iter.Seq2[[]byte, error] {
	return c.cmd.Lines()
}

// StdoutPipe returns a pipe connected to the command's stdout.
func (c *gitCmd) StdoutPipe() (io.ReadCloser, error) {
	return c.cmd.StdoutPipe()
}

// StdinPipe returns a pipe connected to the command's stdin.
func (c *gitCmd) StdinPipe() (io.WriteCloser, error) {
	return c.cmd.StdinPipe()
}

// Kill terminates a started command.
func (c *gitCmd) Kill() error {
	return c.cmd.Kill()
}

// Args reports the command arguments, excluding the git binary name.
func (c *gitCmd) Args() []string {
	return c.cmd.Args()
}

// WithArgs replaces the wrapped command's arguments.
func (c *gitCmd) WithArgs(args ...string) *gitCmd {
	c.cmd.WithArgs(args...)
	return c
}

// WithDir sets the working directory for the wrapped command.
func (c *gitCmd) WithDir(dir string) *gitCmd {
	c.cmd.WithDir(dir)
	return c
}

// WithStdout redirects the command's stdout to the provided writer.
func (c *gitCmd) WithStdout(w io.Writer) *gitCmd {
	c.cmd.WithStdout(w)
	return c
}

// WithStderr redirects the command's stderr to the provided writer.
func (c *gitCmd) WithStderr(w io.Writer) *gitCmd {
	c.cmd.WithStderr(w)
	return c
}

// WithStdin supplies the command's stdin from the provided reader.
func (c *gitCmd) WithStdin(r io.Reader) *gitCmd {
	c.cmd.WithStdin(r)
	return c
}

// WithStdinString supplies the command's stdin from the provided string.
func (c *gitCmd) WithStdinString(s string) *gitCmd {
	c.cmd.WithStdinString(s)
	return c
}

// AppendEnv appends environment variables to the wrapped command.
func (c *gitCmd) AppendEnv(env ...string) *gitCmd {
	c.cmd.AppendEnv(env...)
	return c
}

// CaptureStdout captures stdout for logging
// and inclusion in any returned error.
func (c *gitCmd) CaptureStdout() *gitCmd {
	c.cmd.CaptureStdout()
	return c
}
