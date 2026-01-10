// Package xec is a wrapper around os/exec
// that centralizes command execution.
//
// It provides support for logging command output
// and capturing stderr for error reporting.
//
// # Stderr handling
//
// [Cmd] treats stderr as follows:
//
//   - if the logger is at debug level or lower,
//     stderr for the command will be written directly to the logger
//     with the prefix "$name: " (e.g. "git: ").
//   - if the logger is above debug level,
//     stderr for the command will be captured (up to a limit)
//     and surfaced in the error if the command fails.
//
// This may be customized further with the following methods:
//
//   - use Stderr to redirect stderr elsewhere
//   - use WithLogPrefix to change the prefix for log messages
package xec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"strings"

	"go.abhg.dev/gs/internal/silog"
)

var _osEnviron = os.Environ

// Cmd is an external command being prepared or run.
type Cmd struct {
	cmd     *exec.Cmd
	log     *prefixLogger
	_execer Execer

	// Wraps an error with stderr output.
	wrap func(error) error
}

// Command constructs a Cmd to execute a program with the given arguments.
//
// ctx controls the lifetime of the command,
// and log is used to log command output and errors.
// If log is nil, stderr is buffered and surfaced in the error if the command fails.
func Command(ctx context.Context, log *silog.Logger, name string, args ...string) *Cmd {
	if log == nil {
		log = silog.Nop(&silog.Options{
			Level: silog.LevelInfo,
		})
	}
	logger := &prefixLogger{Logger: log, prefix: name}
	stderr, wrap := outputLogWriter("stderr", logger)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = stderr
	cmd.Env = _osEnviron()
	return &Cmd{
		cmd:     cmd,
		log:     logger,
		wrap:    wrap,
		_execer: DefaultExecer,
	}
}

// WithExecer sets the Execer used to run the command.
// If nil, the DefaultExecer is used.
func (c *Cmd) WithExecer(execer Execer) *Cmd {
	c._execer = execer
	return c
}

func (c *Cmd) execer() Execer {
	if c._execer != nil {
		return c._execer
	}
	return DefaultExecer
}

// Run runs the command, blocking until it completes.
//
// It returns an error if the command fails with a non-zero exit code.
func (c *Cmd) Run() error {
	return c.wrap(c.execer().Run(c.cmd))
}

// Start starts the command, returning immediately.
// It returns an error if the command fails to start.
func (c *Cmd) Start() error {
	return c.wrap(c.execer().Start(c.cmd))
}

// Wait waits for a command started with Start to complete.
// It returns an error if the command fails with a non-zero exit code.
func (c *Cmd) Wait() error {
	return c.wrap(c.execer().Wait(c.cmd))
}

// Kill kills a command started with Start.
func (c *Cmd) Kill() error {
	return c.wrap(c.execer().Kill(c.cmd))
}

// Output runs the command and returns its stdout.
// It returns an error if the command fails with a non-zero exit code.
func (c *Cmd) Output() ([]byte, error) {
	return c.execer().Output(c.cmd)
}

// Args returns the arguments passed to the command,
// not including the command name itself (os.Args[0]).
func (c *Cmd) Args() []string {
	return c.cmd.Args[1:]
}

// WithArgs replaces the arguments passed to the command
// with the given arguments.
//
// args does not include the command name itself.
func (c *Cmd) WithArgs(args ...string) *Cmd {
	c.cmd.Args = append([]string{c.cmd.Args[0]}, args...)
	return c
}

// WithLogPrefix changes the prefixed used for log messages from this command.
func (c *Cmd) WithLogPrefix(prefix string) *Cmd {
	c.log.SetPrefix(prefix)
	return c
}

// WithDir sets the working directory for the command.
func (c *Cmd) WithDir(dir string) *Cmd {
	c.cmd.Dir = dir
	return c
}

// WithStdout redirects the command's stdout to the given writer.
func (c *Cmd) WithStdout(w io.Writer) *Cmd {
	c.cmd.Stdout = w
	return c
}

// CaptureStdout configures the command to also capture stdout (like stderr)
// and surface it either in the logs or in the returned error (if any).
func (c *Cmd) CaptureStdout() *Cmd {
	stdout, wrap := outputLogWriter("stdout", c.log)
	c.cmd.Stdout = stdout
	oldWrap := c.wrap
	c.wrap = func(err error) error {
		return wrap(oldWrap(err))
	}
	return c
}

// StdoutPipe returns a pipe that will be connected to the command's stdout.
func (c *Cmd) StdoutPipe() (io.ReadCloser, error) {
	return c.cmd.StdoutPipe()
}

// WithStderr sets the writer for the command's stderr.
//
// By default, stderr is either logged to the logger
// or captured to be surfaced in the error.
func (c *Cmd) WithStderr(w io.Writer) *Cmd {
	c.cmd.Stderr = w
	// Clear out the stderr wrapping behavior.
	c.wrap = func(err error) error { return err }
	return c
}

// WithStdin supplies the command's stdin from the given reader.
func (c *Cmd) WithStdin(r io.Reader) *Cmd {
	c.cmd.Stdin = r
	return c
}

// WithStdinString supplies the command's stdin from the given string.
func (c *Cmd) WithStdinString(s string) *Cmd {
	return c.WithStdin(strings.NewReader(s))
}

// StdinPipe returns a pipe that will be connected to the command's stdin.
func (c *Cmd) StdinPipe() (io.WriteCloser, error) {
	return c.cmd.StdinPipe()
}

// AppendEnv appends environment variables to the command.
func (c *Cmd) AppendEnv(env ...string) *Cmd {
	// TODO: this is an error prone API.
	// It should be Setenv(key, value string) instead.
	if len(env) == 0 {
		return c
	}

	if c.cmd.Env == nil {
		// This is not likely because we always set it,
		// but worth guarding against anyway.
		c.cmd.Env = os.Environ()
	}
	c.cmd.Env = append(c.cmd.Env, env...)
	return c
}

// OutputChomp runs the command and returns its stdout,
// with trailing whitespace removed.
// It returns an error if the command fails with a non-zero exit code.
func (c *Cmd) OutputChomp() (string, error) {
	out, err := c.Output()
	out, _ = bytes.CutSuffix(out, []byte{'\n'})
	return string(out), err
}

// Lines runs the command and returns its stdout as a sequence of lines.
// See [Scan] for details.
func (c *Cmd) Lines() iter.Seq2[[]byte, error] {
	return c.Scan(bufio.ScanLines)
}

// Scan runs the command and returns its stdout
// as a sequence of tokens split by the given split function.
//
// The byte slice is re-used between iterations
// so the caller must not retain a reference to it.
//
// The byte slice does not include the split delimiter.
//
// If the iteration is stopped early, the command is killed.
//
// If the command exits with a non-zero exit code,
// the error will be returned as the final iteration result.
func (c *Cmd) Scan(split bufio.SplitFunc) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		out, err := c.StdoutPipe()
		if err != nil {
			yield(nil, fmt.Errorf("pipe stdout: %w", err))
			return
		}

		if err := c.Start(); err != nil {
			yield(nil, fmt.Errorf("start: %w", err))
			return
		}

		var finished bool
		defer func() {
			if !finished {
				_ = c.Kill()
			}
		}()

		scanner := bufio.NewScanner(out)
		scanner.Split(split)
		for scanner.Scan() {
			if !yield(scanner.Bytes(), nil) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("scan: %w", err))
			return
		}

		if err := c.Wait(); err != nil {
			// If the command failed, wrap the error with stderr output.
			yield(nil, fmt.Errorf("wait: %w", c.wrap(err)))
			return
		}

		finished = true
	}
}

// Returns an io.Writer that will record an output stream for later use,
// and a wrap function that will wrap an error with the recorded output.
func outputLogWriter(name string, logger *prefixLogger) (w io.Writer, wrap func(error) error) {
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
	var buf bytes.Buffer // TODO: Use a bounded buffer
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
		output := bytes.TrimSpace(buf.Bytes())
		if len(output) == 0 {
			return err
		}

		return errors.Join(err, fmt.Errorf("%s:\n%s", name, output))
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
