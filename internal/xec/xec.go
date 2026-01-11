package xec

import "os/exec"

// ExitError is returned from Wait or Run
// when the command exits with a non-zero exit code.
type ExitError = exec.ExitError

// LookPath searches for an executable named file
// in the directories named by the PATH environment variable.
func LookPath(file string) (string, error) {
	return exec.LookPath(file)
}
