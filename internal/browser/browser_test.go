package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowser_OpenURL(t *testing.T) {
	var called bool
	b := &Browser{
		openURL: func(url string) error {
			called = true
			assert.Equal(t, "https://example.com", url)
			return nil
		},
	}

	require.NoError(t, b.OpenURL("https://example.com"))
	assert.True(t, called)
}

func TestNoop_OpenURL(t *testing.T) {
	var n Noop
	require.NoError(t, n.OpenURL("https://example.com"))
}
