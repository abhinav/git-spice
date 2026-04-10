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

func TestGitCmd_WithExtraConfig(t *testing.T) {
	t.Run("PrependsConfigArgs", func(t *testing.T) {
		cmd := newGitCmd(
			t.Context(), silog.Nop(), _realExec,
			"rebase", "--continue",
		).WithExtraConfig(&extraConfig{
			Editor:             "vim",
			MergeConflictStyle: "zdiff3",
		})

		assert.Equal(t, []string{
			"-c", "core.editor=vim",
			"-c", "merge.conflictStyle=zdiff3",
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
