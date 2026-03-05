// Package git provides access to the Git CLI with a Git library-like
// interface.
//
// All shell-to-Git interactions should be done through this package.
package git

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

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
}

func (ec *extraConfig) args() []string {
	var args []string
	if ec.Editor != "" {
		args = append(args, "-c", "core.editor="+ec.Editor)
	}
	if ec.MergeConflictStyle != "" {
		args = append(args, "-c",
			"merge.conflictStyle="+ec.MergeConflictStyle)
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

// _writeGitCmds is the set of git subcommands
// that may acquire the index lock.
// These commands are retried on index.lock contention.
//
// NOTE: "rebase" is intentionally excluded.
// A failed rebase leaves .git/rebase-merge/ state behind,
// so blindly re-running the command produces
// "rebase already in progress" errors.
// Rebase index.lock recovery is handled at a higher level
// by [Worktree.Rebase].
var _writeGitCmds = map[string]struct{}{
	"checkout":   {},
	"commit":     {},
	"pull":       {},
	"reset":      {},
	"stash":      {},
	"write-tree": {},
}

// Index lock retry configuration.
// Controlled by spice.indexLockTimeout.
var (
	_indexLockMu      sync.RWMutex
	_indexLockTimeout = 5 * time.Second
)

// SetIndexLockTimeout sets the maximum time to spend
// retrying git commands that fail due to index.lock
// contention.
//
// Value semantics match git's core.filesRefLockTimeout:
// 0 disables retry, negative retries indefinitely.
//
// This is typically called once from main
// after reading spice.indexLockTimeout from git config.
func SetIndexLockTimeout(d time.Duration) {
	_indexLockMu.Lock()
	defer _indexLockMu.Unlock()
	_indexLockTimeout = d
}

func indexLockTimeout() time.Duration {
	_indexLockMu.RLock()
	defer _indexLockMu.RUnlock()
	return _indexLockTimeout
}

// isIndexLockErr reports whether err indicates
// that git could not acquire the index lock.
func isIndexLockErr(err error) bool {
	if strings.Contains(err.Error(), "index.lock") {
		return true
	}
	var exitErr *xec.ExitError
	if errors.As(err, &exitErr) {
		return strings.Contains(
			string(exitErr.Stderr), "index.lock",
		)
	}
	return false
}

// firstSubcmd returns the git subcommand
// from the argument list, or "" if none is found.
//
// Git global options like -c key=value and -C dir
// that appear before the subcommand are skipped.
func firstSubcmd(args []string) string {
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
		// -c key=value and -C dir consume
		// the next argument.
		if args[i] == "-c" || args[i] == "-C" {
			i++
		}
	}
	return ""
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
	args ...string,
) *xec.Cmd {
	subcmd := firstSubcmd(args)
	prefix := "git"
	if subcmd != "" {
		prefix += " " + subcmd
	}

	cmd := xec.Command(ctx, log, "git", args...).
		WithExecer(exec).
		WithLogPrefix(prefix)

	if _, ok := _readOnlyGitCmds[subcmd]; ok {
		cmd.AppendEnv("GIT_OPTIONAL_LOCKS=0")
	}

	if _, ok := _writeGitCmds[subcmd]; ok {
		timeout := indexLockTimeout()
		if timeout != 0 {
			cmd.WithRetry(&xec.RetryPolicy{
				Match:     isIndexLockErr,
				Timeout:   timeout,
				BaseDelay: 100 * time.Millisecond,
			})
		}
	}

	return cmd
}
