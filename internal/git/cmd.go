// Package git provides access to the Git CLI with a Git library-like
// interface.
//
// All shell-to-Git interactions should be done through this package.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"go.abhg.dev/gs/internal/silog"
)

// execer controls actual execution of Git commands.
// It provides a single place to hook into for testing.
type execer interface {
	Run(*exec.Cmd) error
	Output(*exec.Cmd) ([]byte, error)
	Start(*exec.Cmd) error
	Wait(*exec.Cmd) error
	Kill(*exec.Cmd) error
}

//go:generate mockgen -source=cmd.go -destination=mock_cmd_test.go -package=git -mock_names=execer=MockExecer -write_package_comment=false -typed

type realExecer struct{}

var _realExec execer = realExecer{}

func (realExecer) Run(cmd *exec.Cmd) error              { return cmd.Run() }
func (realExecer) Output(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }
func (realExecer) Start(cmd *exec.Cmd) error            { return cmd.Start() }
func (realExecer) Wait(cmd *exec.Cmd) error             { return cmd.Wait() }
func (realExecer) Kill(cmd *exec.Cmd) error             { return cmd.Process.Kill() }

type extraConfig struct {
	Editor string // core.editor
}

func (ec *extraConfig) Args() (args []string) {
	if ec == nil {
		return nil
	}
	if ec.Editor != "" {
		args = append(args, "-c", "core.editor="+ec.Editor)
	}
	return args
}

// gitCmd provides a fluent API around exec.Cmd,
// capable of capturing stderr into error objects if it's not being logged.
type gitCmd struct {
	cmd *exec.Cmd
	log *prefixLogger

	// Wraps an error with stderr output.
	wrap func(error) error
}

// newGitCmd builds a new Git command with the given arguments.
// The first argument is the Git subcommand to run.
//
// If the logger is at Debug level or lower,
// stderr of the command will be written to the logger.
// Otherwise, it will be captured and surfaced in the error
// if the command fails.
//
// This allows for a nicer, less noisy UX for expected errors:
//
//   - if a Git command was expected to fail, and the error is never logged,
//     its stderr output will not be shown to the user.
//   - if the error is logged, the stderr output will be shown to the user.
//   - if the program is running in verbose mode,
//     the stderr output will always be shown to the user,
//     but it won't be duplicated in the error message.
func newGitCmd(ctx context.Context, log *silog.Logger, cfg *extraConfig, args ...string) *gitCmd {
	prefix := "git"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		prefix += " " + args[0]
	}

	logger := &prefixLogger{Logger: log, prefix: prefix}

	args = append(cfg.Args(), args...)
	stderr, wrap := stderrWriter(logger)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stderr = stderr

	return &gitCmd{
		cmd:  cmd,
		log:  logger,
		wrap: wrap,
	}
}

// LogPrefix changes the prefixed used for log messages from this command.
// Defaults to "git $arg" where $arg is the first argument of the command.
func (c *gitCmd) LogPrefix(s string) *gitCmd {
	c.log.SetPrefix(s)
	return c
}

// Dir sets the working directory for the command.
func (c *gitCmd) Dir(dir string) *gitCmd {
	c.cmd.Dir = dir
	return c
}

// Stdout sets the writer for the command's stdout.
func (c *gitCmd) Stdout(w io.Writer) *gitCmd {
	c.cmd.Stdout = w
	return c
}

// Stderr sets the writer for the command's stderr.
//
// By default, stderr is either logged to the logger
// or captured to be surfaced in the error.
func (c *gitCmd) Stderr(w io.Writer) *gitCmd {
	c.cmd.Stderr = w
	c.wrap = func(err error) error { return err }
	return c
}

// Stdin supplies the command's stdin from the given reader.
func (c *gitCmd) Stdin(r io.Reader) *gitCmd {
	c.cmd.Stdin = r
	return c
}

// StdinString supplies the command's stdin from the given string.
func (c *gitCmd) StdinString(s string) *gitCmd {
	return c.Stdin(strings.NewReader(s))
}

// AppendEnv appends environment variables to the command.
func (c *gitCmd) AppendEnv(env ...string) *gitCmd {
	// TODO: this is an error prone API.
	// It should be Setenv(key, value string) instead.
	if len(env) == 0 {
		return c
	}

	if c.cmd.Env == nil {
		c.cmd.Env = os.Environ()
	}
	c.cmd.Env = append(c.cmd.Env, env...)
	return c
}

// StdoutPipe returns a pipe that will be connected to the command's stdout.
func (c *gitCmd) StdoutPipe() (io.ReadCloser, error) {
	return c.cmd.StdoutPipe()
}

// StdinPipe returns a pipe that will be connected to the command's stdin.
func (c *gitCmd) StdinPipe() (io.WriteCloser, error) {
	return c.cmd.StdinPipe()
}

// Run runs the command, blocking until it completes.
//
// It returns an error if the command fails with a non-zero exit code.
func (c *gitCmd) Run(exec execer) error {
	return c.wrap(exec.Run(c.cmd))
}

// Start starts the command, returning immediately.
// It returns an error if the command fails to start.
func (c *gitCmd) Start(exec execer) error {
	return c.wrap(exec.Start(c.cmd))
}

// Wait waits for a command started with Start to complete.
// It returns an error if the command fails with a non-zero exit code.
func (c *gitCmd) Wait(exec execer) error {
	return c.wrap(exec.Wait(c.cmd))
}

// Kill kills a command started with Start.
func (c *gitCmd) Kill(exec execer) error {
	return c.wrap(exec.Kill(c.cmd))
}

// Output runs the command and returns its stdout.
// It returns an error if the command fails with a non-zero exit code.
func (c *gitCmd) Output(exec execer) ([]byte, error) {
	out, err := exec.Output(c.cmd)
	return out, c.wrap(err)
}

// OutputString runs the command and returns its stdout as a string,
// with the trailing newline removed.
// It returns an error if the command fails with a non-zero exit code.
func (c *gitCmd) OutputString(exec execer) (string, error) {
	out, err := c.Output(exec)
	out, _ = bytes.CutSuffix(out, []byte{'\n'})
	return string(out), err
}

// Returns an io.Writer that will record sterr for later use,
// and a wrap function that will wrap an error with the recorded
// stderr output.
func stderrWriter(logger *prefixLogger) (w io.Writer, wrap func(error) error) {
	if logger.Level() <= silog.LevelDebug {
		// If logging is enabled, return an io.Writer
		// that writes to the logger.
		w, flush := silog.Writer(logger, silog.LevelDebug)
		return w, func(err error) error {
			flush()
			return err
		}
	}

	// Otherwise, buffer it all in-memory to put into the error.
	var buf bytes.Buffer
	return &buf, func(err error) error {
		if err == nil {
			return err
		}

		// We can't check buf.Bytes if err == nil
		// because it may be called while the command is still running
		// (e.g. in Start).
		//
		// err != nil guarantees that the operation has finished
		// because the command has exited with an error.
		stderr := bytes.TrimSpace(buf.Bytes())
		if len(stderr) == 0 {
			return err
		}

		return errors.Join(err, fmt.Errorf("stderr:\n%s", stderr))
	}
}

type prefixLogger struct {
	*silog.Logger

	prefix string
}

var _ silog.LeveledLogger = (*prefixLogger)(nil)

func (pl *prefixLogger) SetPrefix(prefix string) {
	pl.prefix = prefix
}

func (pl *prefixLogger) Log(level silog.Level, msg string, kvs ...any) {
	if pl.prefix != "" {
		msg = pl.prefix + ": " + msg
	}
	pl.Logger.Log(level, msg, kvs...)
}
