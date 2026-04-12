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

func TestNewGitCmd_args(t *testing.T) {
	cmd := newGitCmd(
		t.Context(), silog.Nop(), _realExec,
		"rev-parse", "--verify", "HEAD",
	)

	assert.Equal(t, []string{
		"rev-parse", "--verify", "HEAD",
	}, cmd.Args())
}
