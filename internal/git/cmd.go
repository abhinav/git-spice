// Package git provides access to the Git CLI with a Git library-like
// interface.
//
// All shell-to-Git interactions should be done through this package.
package git

import (
	"context"
	"strings"

	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
)

// execer controls actual execution of Git commands.
// It provides a single place to hook into for testing.
type execer = xec.Execer

var _realExec = xec.DefaultExecer

//go:generate mockgen -destination=mock_cmd_test.go -package=git -mock_names=execer=MockExecer -write_package_comment=false -typed . execer

type extraConfig struct {
	Editor string // core.editor

	MergeConflictStyle string // merge.conflictStyle
}

func (ec *extraConfig) args() []string {
	var args []string
	if ec.Editor != "" {
		args = append(args, "-c", "core.editor="+ec.Editor)
	}
	if ec.MergeConflictStyle != "" {
		args = append(args, "-c", "merge.conflictStyle="+ec.MergeConflictStyle)
	}
	return args
}

func (ec *extraConfig) WithArgs(cmd *xec.Cmd) *xec.Cmd {
	if ec == nil {
		return nil
	}

	newArgs := ec.args()
	if len(newArgs) == 0 {
		return cmd
	}

	newArgs = append(newArgs, cmd.Args()...)
	return cmd.WithArgs(newArgs...)
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
func newGitCmd(ctx context.Context, log *silog.Logger, exec execer, args ...string) *xec.Cmd {
	prefix := "git"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		prefix += " " + args[0]
	}

	return xec.Command(ctx, log, "git", args...).
		WithExecer(exec).
		WithLogPrefix(prefix)
}
