package xec

import "os/exec"

// TODO: processor might be a better name for this
// since it controls the process lifecycle?

//go:generate mockgen -destination=xectest/mock_execer.go -package=xectest -write_package_comment=false -typed . Execer

// Execer controls actual execution of commands.
// It provides a way to intercept command execution for testing.
type Execer interface {
	Output(*exec.Cmd) ([]byte, error)
	Run(*exec.Cmd) error
	Start(*exec.Cmd) error
	Wait(*exec.Cmd) error
	Kill(*exec.Cmd) error
}

type realExecer struct{}

// DefaultExecer is the default implementation of Execer.
// It uses the real os/exec package to execute commands.
var DefaultExecer Execer = realExecer{}

func (realExecer) Run(cmd *exec.Cmd) error              { return cmd.Run() }
func (realExecer) Start(cmd *exec.Cmd) error            { return cmd.Start() }
func (realExecer) Wait(cmd *exec.Cmd) error             { return cmd.Wait() }
func (realExecer) Kill(cmd *exec.Cmd) error             { return cmd.Process.Kill() }
func (realExecer) Output(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }
