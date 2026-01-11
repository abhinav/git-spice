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
