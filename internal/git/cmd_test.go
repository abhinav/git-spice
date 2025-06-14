package git

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/silog"
)

func TestGitCmd_logPrefix(t *testing.T) {
	var logBuffer bytes.Buffer
	log := silog.New(&logBuffer, &silog.Options{
		Level: silog.LevelDebug,
	})

	t.Run("DefaultPrefixNoCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, "--unknown-flag").
			Dir(t.TempDir()).
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " git: ")
	})

	t.Run("DefaultPrefixCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, "unknown-cmd").
			Dir(t.TempDir()).
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " git unknown-cmd: ")
	})

	t.Run("LogPrefixAfterwards", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, "whatever").
			Dir(t.TempDir()).
			LogPrefix("different").
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " different: ")
	})
}
