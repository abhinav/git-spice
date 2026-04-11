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
