package xec

import "os/exec"

// EditCommand constructs a command to open the editor
// with the given editor command.
// The editor command may be a shell command or a binary name.
func EditCommand(editCmd string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if exe, err := LookPath(editCmd); err == nil {
		cmd = exec.Command(exe, args...)
	} else {
		// We'll run:
		//   sh -c 'EDITOR "$@"' -- "$1" "$2" ...
		// The shell will take care of quoting issues.
		args = append([]string{"-c", editCmd + ` "$@"`, "--"}, args...)
		cmd = exec.Command("sh", args...)
	}
	return cmd
}
