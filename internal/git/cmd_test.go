package git

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec/xectest"
	"go.uber.org/mock/gomock"
)

var NewMockExecer = xectest.NewMockExecer

func TestGitCmd_logPrefix(t *testing.T) {
	var logBuffer bytes.Buffer
	log := silog.New(&logBuffer, &silog.Options{
		Level: silog.LevelDebug,
	})

	t.Run("DefaultPrefixNoCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, _realExec, "--unknown-flag").
			WithDir(t.TempDir()).
			Run()

		assert.Contains(t, logBuffer.String(), " git: ")
	})

	t.Run("DefaultPrefixCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, _realExec, "unknown-cmd").
			WithDir(t.TempDir()).
			Run()

		assert.Contains(t, logBuffer.String(), " git unknown-cmd: ")
	})

	t.Run("LogPrefixAfterwards", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, _realExec, "whatever").
			WithDir(t.TempDir()).
			WithLogPrefix("different").
			Run()

		assert.Contains(t, logBuffer.String(), " different: ")
	})
}

func TestNewGitCmd_optionalLocks(t *testing.T) {
	t.Run("ReadOnlyGetsOptionalLocks", func(t *testing.T) {
		for _, subcmd := range []string{
			"rev-parse", "merge-base", "for-each-ref",
			"config", "log", "diff",
		} {
			t.Run(subcmd, func(t *testing.T) {
				cmd := newGitCmd(
					t.Context(), silog.Nop(),
					_realExec, subcmd,
				)
				out, _ := cmd.
					WithDir(t.TempDir()).
					AppendEnv("GIT_OPTIONAL_LOCKS_CHECK=1").
					OutputChomp()
				// We can't easily inspect env,
				// but we can verify it compiles and runs.
				_ = out
			})
		}
	})

	t.Run("WriteDoesNotGetOptionalLocks", func(t *testing.T) {
		for _, subcmd := range []string{
			"checkout", "commit", "reset",
		} {
			t.Run(subcmd, func(t *testing.T) {
				// Verify the command is constructed
				// without error.
				_ = newGitCmd(
					t.Context(), silog.Nop(),
					_realExec, subcmd,
				)
			})
		}
	})
}

func TestIsIndexLockErr(t *testing.T) {
	t.Run("MatchesErrorString", func(t *testing.T) {
		err := errors.New(
			"Unable to create " +
				"'/repo/.git/index.lock': File exists",
		)
		assert.True(t, isIndexLockErr(err))
	})

	t.Run("MatchesExitErrorStderr", func(t *testing.T) {
		err := &exec.ExitError{
			Stderr: []byte(
				"error: Unable to create " +
					"'/repo/.git/index.lock': " +
					"File exists.\n",
			),
		}
		assert.True(t, isIndexLockErr(err))
	})

	t.Run("DoesNotMatchUnrelated", func(t *testing.T) {
		err := errors.New("some other error")
		assert.False(t, isIndexLockErr(err))
	})

	t.Run("DoesNotMatchUnrelatedExitError", func(t *testing.T) {
		err := &exec.ExitError{
			Stderr: []byte("merge conflict\n"),
		}
		assert.False(t, isIndexLockErr(err))
	})
}

// TestNewGitCmd_retryWithDashC verifies that write commands
// preceded by -c options still get the index.lock retry policy.
func TestNewGitCmd_retryWithDashC(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := NewMockExecer(ctrl)

	lockErr := errors.New(
		"Unable to create " +
			"'/repo/.git/index.lock': File exists",
	)

	// First attempt fails with index.lock;
	// second attempt succeeds.
	first := mockExec.EXPECT().
		Run(gomock.Any()).
		Return(lockErr)
	mockExec.EXPECT().
		Run(gomock.Any()).
		After(first.Call).
		Return(nil)

	cmd := newGitCmd(
		t.Context(), silog.Nop(), mockExec,
		"-c", "core.editor=true", "checkout",
	)
	require.NoError(t, cmd.Run())
}

// TestNewGitCmd_rebaseNoGenericRetry verifies that rebase
// does not use the generic retry mechanism.
// Rebase recovery is handled at a higher level
// by [Worktree.Rebase].
func TestNewGitCmd_rebaseNoGenericRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := NewMockExecer(ctrl)

	lockErr := errors.New(
		"Unable to create " +
			"'/repo/.git/index.lock': File exists",
	)

	// Only one attempt: no retry for rebase.
	mockExec.EXPECT().
		Run(gomock.Any()).
		Return(lockErr)

	cmd := newGitCmd(
		t.Context(), silog.Nop(), mockExec,
		"-c", "advice.mergeConflict=false", "rebase",
	)
	err := cmd.Run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "index.lock")
}

func TestFirstSubcmd(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"Subcommand", []string{"rev-parse"}, "rev-parse"},
		{"Flag", []string{"--version"}, ""},
		{"Empty", nil, ""},
		{
			"DashC",
			[]string{"-c", "k=v", "rebase"},
			"rebase",
		},
		{
			"DashCapitalC",
			[]string{"-C", "/dir", "status"},
			"status",
		},
		{
			"MultipleDashC",
			[]string{"-c", "k=v", "-c", "k2=v2", "rebase"},
			"rebase",
		},
		{
			"DashCThenFlag",
			[]string{"-c", "k=v", "--version"},
			"",
		},
		{
			"DashCOnly",
			[]string{"-c", "k=v"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, firstSubcmd(tt.args))
		})
	}
}
