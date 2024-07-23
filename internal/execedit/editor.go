// Package execedit provides the ability to invoke external editors.
package execedit

import (
	"os"
	"os/exec"
)

// Command constructs a command to open the editor
// with the given editor command.
// The editor command may be a shell command or a binary name.
func Command(edit string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if exe, err := exec.LookPath(edit); err == nil {
		cmd = exec.Command(exe, args...)
	} else {
		// We'll run:
		//   sh -c 'EDITOR "$@"' -- "$1" "$2" ...
		// The shell will take care of quoting issues.
		args = append([]string{"-c", edit + ` "$@"`, "--"}, args...)
		cmd = exec.Command("sh", args...)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
