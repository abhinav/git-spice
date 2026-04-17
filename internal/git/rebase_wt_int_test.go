package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
	"go.uber.org/mock/gomock"
)

// TestRebase_issue1083_lsFilesError covers
// https://github.com/abhinav/git-spice/issues/1083.
//
// If the post-rebase 'git ls-files --unmerged' probe fails,
// Rebase must surface that probe error directly.
// Before the fix, the iterator error was ignored,
// and the zero-value path was treated like a real file.
// That produced a blank bullet in the user-facing conflict report.
func TestRebase_issue1083_lsFilesError(t *testing.T) {
	installFakeGit(t)
	t.Setenv("GIT_ISSUE_1083_HELPER", "1")

	var logBuf bytes.Buffer
	log := silog.New(&logBuf, nil)

	_, wt := NewFakeRepositoryWithLogger(t, "", _realExec, log)

	err := wt.Rebase(t.Context(), RebaseRequest{
		Branch:    "feature",
		Upstream:  "main",
		Autostash: true,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "list unmerged files")
	assert.ErrorContains(t, err, "git ls-files")
	assert.NotErrorAs(t, err, new(*RebaseInterruptError))
	assert.NotContains(t, err.Error(), "dirty changes could not be re-applied")
	assert.Empty(t, logBuf.String())
}

func TestRebase_interactiveRetryPreservesTerminal(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := NewMockExecer(ctrl)
	_, wt := newFakeRepositoryWithCommonOptions(t, "", commonOptions{
		exec:             mockExec,
		indexLockTimeout: 200 * time.Millisecond,
	})

	lockPath := filepath.Join(wt.gitDir, "index.lock")
	require.NoError(t, os.WriteFile(lockPath, nil, 0o644))
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.Remove(lockPath)
	}()

	mockExec.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			_, _ = fmt.Fprintln(cmd.Stderr, "fatal: Unable to create '.git/index.lock'")
			return &exec.ExitError{}
		})

	mockExec.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			assert.Same(t, os.Stdin, cmd.Stdin)
			assert.Same(t, os.Stdout, cmd.Stdout)
			assert.NotNil(t, cmd.Stderr)
			return nil
		})

	err := wt.Rebase(t.Context(), RebaseRequest{
		Branch:      "feature",
		Upstream:    "main",
		Interactive: true,
	})
	require.NoError(t, err)
}

func TestRebase_zeroIndexLockTimeoutDisablesRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := NewMockExecer(ctrl)
	_, wt := newFakeRepositoryWithCommonOptions(t, "", commonOptions{
		exec:             mockExec,
		indexLockTimeout: 0,
	})

	mockExec.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			_, _ = fmt.Fprintln(cmd.Stderr,
				"fatal: Unable to create '.git/index.lock'")
			return &exec.ExitError{}
		}).
		Times(1)

	err := wt.Rebase(t.Context(), RebaseRequest{
		Branch:   "feature",
		Upstream: "main",
	})
	require.Error(t, err)
	assert.ErrorAs(t, err, new(*xec.ExitError))
}

func TestRebase_recoveryFailureReturnsRecoveryErr(t *testing.T) {
	installFakeGit(t)
	t.Setenv("GIT_ISSUE_1083_HELPER", "rebase-recovery-failure")
	t.Setenv("GIT_REBASE_RECOVERY_MARKER",
		filepath.Join(t.TempDir(), "rebase-recovery-marker"))

	log := silog.Nop(&silog.Options{Level: silog.LevelInfo})
	_, wt := newFakeRepositoryWithCommonOptions(t, "", commonOptions{
		log:              log,
		exec:             _realExec,
		indexLockTimeout: 200 * time.Millisecond,
	})

	lockPath := filepath.Join(wt.gitDir, "index.lock")
	require.NoError(t, os.WriteFile(lockPath, nil, 0o644))
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.Remove(lockPath)
	}()

	err := wt.Rebase(t.Context(), RebaseRequest{
		Branch:   "feature",
		Upstream: "main",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "fatal: recovery failed")
	assert.NotContains(t, err.Error(), "index.lock")
}

func gitIssue1083() {
	subcommand := ""
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-c" {
			i++
			continue
		}
		subcommand = os.Args[i]
		break
	}

	switch subcommand {
	case "rebase":
		// Skip the real rebase machinery.
		// This test only exercises failure in the follow-up ls-files probe.
		return
	case "ls-files":
		fmt.Fprintln(os.Stderr, "fatal: synthetic ls-files failure")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "unexpected git command %q: %v\n",
			subcommand, os.Args)
		os.Exit(1)
	}
}

func gitRebaseRecoveryFailure() {
	marker := os.Getenv("GIT_REBASE_RECOVERY_MARKER")
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(marker, nil, 0o644)
		fmt.Fprintln(os.Stderr, "fatal: Unable to create '.git/index.lock'")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "fatal: recovery failed")
	os.Exit(1)
}

func installFakeGit(t testing.TB) {
	t.Helper()

	dir := t.TempDir()
	exe, err := os.Executable()
	require.NoError(t, err)

	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	require.NoError(t, os.Symlink(exe, filepath.Join(dir, name)))

	t.Setenv("PATH", dir+string(filepath.ListSeparator)+os.Getenv("PATH"))
}
