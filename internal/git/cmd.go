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

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/ioutil"
)

type execer interface {
	Run(*exec.Cmd) error
	Output(*exec.Cmd) ([]byte, error)
	Start(*exec.Cmd) error
	Wait(*exec.Cmd) error
	Kill(*exec.Cmd) error
}

type realExecer struct{}

var _realExec execer = realExecer{}

func (realExecer) Run(cmd *exec.Cmd) error              { return cmd.Run() }
func (realExecer) Output(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }
func (realExecer) Start(cmd *exec.Cmd) error            { return cmd.Start() }
func (realExecer) Wait(cmd *exec.Cmd) error             { return cmd.Wait() }
func (realExecer) Kill(cmd *exec.Cmd) error             { return cmd.Process.Kill() }

// gitCmd provides a fluent API around exec.Cmd,
// unconditionally capturing stderr into errors.
type gitCmd struct {
	cmd *exec.Cmd

	// Wraps an error with stderr output.
	wrap func(error) error
}

func newGitCmd(ctx context.Context, log *log.Logger, args ...string) *gitCmd {
	name := "git"
	if len(args) > 0 {
		name += " " + args[0]
	}

	stderr, wrap := stderrWriter(name, log)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stderr = stderr

	return &gitCmd{
		cmd:  cmd,
		wrap: wrap,
	}
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
// It returns an error if the command fails with a non-zero exit code.
func (c *gitCmd) Run(exec execer) error {
	return c.wrap(exec.Run(c.cmd))
}

// Start starts the command, returning immediately.
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

// cmdStdinWriter is an io.WriteCloser that writes to a command's stdin,
// and upon closure, closes the stdin stream and waits for the command to exit.
type cmdStdinWriter struct {
	cmd   *gitCmd
	exec  execer
	stdin io.WriteCloser
}

var _ io.WriteCloser = (*cmdStdinWriter)(nil)

func (w *cmdStdinWriter) Write(p []byte) (n int, err error) {
	return w.stdin.Write(p)
}

func (w *cmdStdinWriter) Close() error {
	err := w.stdin.Close()
	if err != nil {
		return errors.Join(err, w.cmd.Kill(w.exec))
	}
	return w.cmd.Wait(w.exec)
}

// Returns an io.Writer that will record sterr for later use,
// and a wrap function that will wrap an error with the recorded
// stderr output.
func stderrWriter(cmd string, logger *log.Logger) (w io.Writer, wrap func(error) error) {
	if logger.GetLevel() <= log.DebugLevel {
		// If logging is enabled, return an io.Writer
		// that writes to the logger.
		cmdLog := logger.WithPrefix(cmd)
		w, flush := ioutil.LogWriter(cmdLog, log.DebugLevel)
		return w, func(err error) error {
			flush()
			return err
		}
	}

	// Otherwise, buffer it all in-memory to put into the error.
	var buf bytes.Buffer
	return &buf, func(err error) error {
		stderr := bytes.TrimSpace(buf.Bytes())
		if err == nil || len(stderr) == 0 {
			return err
		}

		return errors.Join(err, fmt.Errorf("stderr:\n%s", stderr))
	}
}
