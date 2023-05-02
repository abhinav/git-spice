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
	"log"
	"os/exec"

	"go.abhg.dev/git-stack/internal/ioutil"
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

func (c *gitCmd) Dir(dir string) *gitCmd {
	c.cmd.Dir = dir
	return c
}

func (c *gitCmd) Stdout(w io.Writer) *gitCmd {
	c.cmd.Stdout = w
	return c
}

func (c *gitCmd) StdoutPipe() (io.ReadCloser, error) {
	return c.cmd.StdoutPipe()
}

func (c *gitCmd) Run(exec execer) error {
	return c.wrap(exec.Run(c.cmd))
}

func (c *gitCmd) Start() error {
	return c.wrap(c.cmd.Start())
}

func (c *gitCmd) Output(exec execer) ([]byte, error) {
	out, err := exec.Output(c.cmd)
	return out, c.wrap(err)
}

func (c *gitCmd) OutputString(exec execer) (string, error) {
	out, err := c.Output(exec)
	out, _ = bytes.CutSuffix(out, []byte{'\n'})
	return string(out), err
}

func (c *gitCmd) Wait(exec execer) error {
	return c.wrap(exec.Wait(c.cmd))
}

func (c *gitCmd) Kill(exec execer) error {
	return c.wrap(exec.Kill(c.cmd))
}

// Returns an io.Writer that will record sterr for later use,
// and a wrap function that will wrap an error with the recorded
// stderr output.
func stderrWriter(cmd string, log *log.Logger) (w io.Writer, wrap func(error) error) {
	isDiscard := log == nil || log.Writer() == io.Discard
	if !isDiscard {
		// If logging is enabled, return an io.Writer
		// that writes to the logger.
		w, flush := ioutil.LogWriter(log, cmd+": ")
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
