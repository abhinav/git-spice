package claude

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindClaudeBinary(t *testing.T) {
	t.Run("FindsInstalledBinary", func(t *testing.T) {
		path, err := FindClaudeBinary()
		if err != nil {
			t.Skip("Claude CLI not installed, skipping test")
		}
		assert.NotEmpty(t, path)
		t.Logf("Found claude at: %s", path)
	})
}

func TestClient_checkStderr(t *testing.T) {
	t.Run("AuthError", func(t *testing.T) {
		stderr := "Error: Not authenticated. Please run 'claude auth' first."
		err := checkStderr(stderr)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotAuthenticated))
	})

	t.Run("RateLimitError", func(t *testing.T) {
		stderr := "Error: Rate limit exceeded. Please try again later."
		err := checkStderr(stderr)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrRateLimited))
	})

	t.Run("NoError", func(t *testing.T) {
		stderr := ""
		err := checkStderr(stderr)
		assert.NoError(t, err)
	})

	t.Run("UnknownError", func(t *testing.T) {
		stderr := "Some other error message"
		err := checkStderr(stderr)
		require.Error(t, err)

		var claudeErr *Error
		assert.True(t, errors.As(err, &claudeErr))
		assert.Contains(t, claudeErr.Message, "Some other error")
	})
}

func TestNewClient(t *testing.T) {
	t.Run("DefaultOptions", func(t *testing.T) {
		client := NewClient(nil)
		assert.NotNil(t, client)
	})

	t.Run("CustomOptions", func(t *testing.T) {
		opts := &ClientOptions{
			BinaryPath: "/custom/path/to/claude",
		}
		client := NewClient(opts)
		assert.Equal(t, "/custom/path/to/claude", client.binaryPath)
	})
}

func TestClient_SendPrompt_notInstalled(t *testing.T) {
	// Test behavior when Claude is not installed.
	client := NewClient(&ClientOptions{
		BinaryPath: "/nonexistent/claude",
	})

	_, err := client.SendPrompt(context.Background(), "test prompt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotInstalled))
}
