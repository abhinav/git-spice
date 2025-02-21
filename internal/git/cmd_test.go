package git

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
)

func TestGitCmd_logPrefix(t *testing.T) {
	var logBuffer bytes.Buffer
	log := log.NewWithOptions(&logBuffer, log.Options{
		Level: log.DebugLevel,
	})

	t.Run("DefaultPrefixNoCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, nil, "--unknown-flag").
			Dir(t.TempDir()).
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " git: ")
	})

	t.Run("DefaultPrefixCommand", func(t *testing.T) {
		defer logBuffer.Reset()

		_ = newGitCmd(t.Context(), log, nil, "unknown-cmd").
			Dir(t.TempDir()).
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " git unknown-cmd: ")
	})

	t.Run("PriorPrefix", func(t *testing.T) {
		defer logBuffer.Reset()

		log := log.WithPrefix("custom")
		_ = newGitCmd(t.Context(), log, nil, "whatever").
			Dir(t.TempDir()).
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " custom: ")
	})

	t.Run("LogPrefixAfterwards", func(t *testing.T) {
		defer logBuffer.Reset()

		log := log.WithPrefix("custom")
		_ = newGitCmd(t.Context(), log, nil, "whatever").
			Dir(t.TempDir()).
			LogPrefix("different").
			Run(_realExec)

		assert.Contains(t, logBuffer.String(), " different: ")
	})
}
