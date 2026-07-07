package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestValidateRemoteURL(t *testing.T) {
	remoteURL, err := giturl.Parse("ssh://git@ssh.example.com:2222/owner/repo")
	require.NoError(t, err)

	t.Run("EmptyConfiguredURL", func(t *testing.T) {
		assert.NoError(t, ValidateRemoteURL("", remoteURL))
	})

	t.Run("MatchingSubdomain", func(t *testing.T) {
		assert.NoError(t, ValidateRemoteURL("https://example.com", remoteURL))
	})

	t.Run("MatchingPort", func(t *testing.T) {
		assert.NoError(t, ValidateRemoteURL("https://example.com:2222", remoteURL))
	})

	t.Run("MissingRemoteURL", func(t *testing.T) {
		err := ValidateRemoteURL("https://example.com", nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedURL)
	})

	t.Run("WrongHost", func(t *testing.T) {
		err := ValidateRemoteURL("https://example.org", remoteURL)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedURL)
	})

	t.Run("WrongPort", func(t *testing.T) {
		err := ValidateRemoteURL("https://example.com:443", remoteURL)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedURL)
	})
}

func TestRemoteURLMatches(t *testing.T) {
	tests := []struct {
		name      string
		forgeURL  string
		remoteURL string
		want      bool
	}{
		{
			name:      "SameHost",
			forgeURL:  "https://example.com",
			remoteURL: "https://example.com/owner/repo",
			want:      true,
		},
		{
			name:      "Subdomain",
			forgeURL:  "https://example.com",
			remoteURL: "ssh://git@ssh.example.com/owner/repo",
			want:      true,
		},
		{
			name:      "RemotePortAllowedWithoutBasePort",
			forgeURL:  "https://example.com",
			remoteURL: "ssh://git@example.com:2222/owner/repo",
			want:      true,
		},
		{
			name:      "BasePortMustMatch",
			forgeURL:  "https://example.com:443",
			remoteURL: "ssh://git@example.com:2222/owner/repo",
			want:      false,
		},
		{
			name:      "WrongHost",
			forgeURL:  "https://example.com",
			remoteURL: "https://example.org/owner/repo",
			want:      false,
		},
		{
			name:      "InvalidBaseURL",
			forgeURL:  "NOT\tA\nVALID URL",
			remoteURL: "https://example.com/owner/repo",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			assert.Equal(t, tt.want, remoteURLMatches(tt.forgeURL, remoteURL))
		})
	}

	t.Run("NilRemoteURL", func(t *testing.T) {
		assert.False(t, remoteURLMatches("https://example.com", nil))
	})
}
