package github

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLITokenSource(t *testing.T) {
	ts := &CLITokenSource{
		cmdOutput: func(*exec.Cmd) ([]byte, error) {
			return []byte("mytoken\n"), nil
		},
	}

	token, err := ts.Token()
	require.NoError(t, err)
	assert.Equal(t, "mytoken", token.AccessToken)

	t.Run("error", func(t *testing.T) {
		ts := &CLITokenSource{
			cmdOutput: func(*exec.Cmd) ([]byte, error) {
				return nil, assert.AnError
			},
		}

		_, err := ts.Token()
		require.Error(t, err)
		assert.ErrorIs(t, err, assert.AnError)
	})
}
