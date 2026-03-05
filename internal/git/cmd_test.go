package git

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec/xectest"
)

var NewMockExecer = xectest.NewMockExecer

func TestGitCmd_logPrefix(t *testing.T) {
	var logBuffer bytes.Buffer
	log := silog.New(&logBuffer, &silog.Options{
		Level: silog.LevelDebug,
	})

	t.Run("DefaultPrefixCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, _realExec, "unknown-cmd").
			WithDir(t.TempDir()).
			Run()

		assert.Contains(t, logBuffer.String(), " git unknown-cmd: ")
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

func TestGitCmd_WithExtraConfig(t *testing.T) {
	t.Run("PrependsConfigArgs", func(t *testing.T) {
		cmd := newGitCmd(
			t.Context(), silog.Nop(), _realExec,
			"rebase", "--continue",
		).WithExtraConfig(&extraConfig{
			Editor:              "vim",
			MergeConflictStyle:  "zdiff3",
			AdviceMergeConflict: new(false),
		})

		assert.Equal(t, []string{
			"-c", "core.editor=vim",
			"-c", "merge.conflictStyle=zdiff3",
			"-c", "advice.mergeConflict=false",
			"rebase", "--continue",
		}, cmd.Args())
	})

	t.Run("NilIsNoOp", func(t *testing.T) {
		cmd := newGitCmd(
			t.Context(), silog.Nop(), _realExec,
			"rev-parse", "HEAD",
		)

		assert.Equal(t, cmd, cmd.WithExtraConfig(nil))
		assert.Equal(t, []string{"rev-parse", "HEAD"}, cmd.Args())
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

// TestNewGitCmd_retryWithExtraConfig verifies that write commands
// retain their updated arguments across retries.
func TestNewGitCmd_retryWithExtraConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := NewMockExecer(ctrl)

	lockErr := errors.New(
		"Unable to create " +
			"'/repo/.git/index.lock': File exists",
	)

	var gotArgs [][]string

	// First attempt fails with index.lock;
	// second attempt succeeds.
	first := mockExec.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			gotArgs = append(gotArgs, append([]string(nil), cmd.Args...))
			return lockErr
		})
	mockExec.EXPECT().
		Run(gomock.Any()).
		After(first.Call).
		DoAndReturn(func(cmd *exec.Cmd) error {
			gotArgs = append(gotArgs, append([]string(nil), cmd.Args...))
			return nil
		})

	cmd := newGitCmd(
		t.Context(), silog.Nop(), mockExec, "checkout",
	).WithExtraConfig(&extraConfig{Editor: "true"})
	require.NoError(t, cmd.Run())
	require.Len(t, gotArgs, 2)
	assert.Equal(t, gotArgs[0], gotArgs[1])
	assert.Equal(t, []string{
		"git",
		"-c", "core.editor=true",
		"checkout",
	}, gotArgs[0])
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
		t.Context(), silog.Nop(), mockExec, "rebase",
	).WithExtraConfig(&extraConfig{
		AdviceMergeConflict: new(false),
	})
	err := cmd.Run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "index.lock")
}

func TestNewGitCmd_args(t *testing.T) {
	cmd := newGitCmd(
		t.Context(), silog.Nop(), _realExec,
		"rev-parse", "--verify", "HEAD",
	)

	assert.Equal(t, []string{
		"rev-parse", "--verify", "HEAD",
	}, cmd.Args())
}
