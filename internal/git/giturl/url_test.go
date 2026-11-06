package giturl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		give         string
		wantScheme   string
		wantPath     string
		wantHostname string
		wantPort     string
	}{
		{
			name:         "HTTPS",
			give:         "https://github.com/owner/repo",
			wantScheme:   "https",
			wantPath:     "/owner/repo",
			wantHostname: "github.com",
		},
		{
			name:         "SSHProtocol",
			give:         "ssh://git@github.com/owner/repo",
			wantScheme:   "ssh",
			wantPath:     "/owner/repo",
			wantHostname: "github.com",
		},
		{
			name:         "SCPStyleSSH",
			give:         "git@github.com:owner/repo",
			wantScheme:   "ssh",
			wantPath:     "/owner/repo",
			wantHostname: "github.com",
		},
		{
			name:         "SSHWithPort",
			give:         "ssh://git@ssh.github.com:443/owner/repo",
			wantScheme:   "ssh",
			wantPath:     "/owner/repo",
			wantHostname: "ssh.github.com",
			wantPort:     "443",
		},
		{
			name:         "GitProtocol",
			give:         "git://github.com/owner/repo.git",
			wantScheme:   "git",
			wantPath:     "/owner/repo.git",
			wantHostname: "github.com",
		},
		{
			name:         "GitHTTPS",
			give:         "git+https://github.com/owner/repo",
			wantScheme:   "git+https",
			wantPath:     "/owner/repo",
			wantHostname: "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.give)
			require.NoError(t, err)

			assert.Equal(t, tt.give, got.Raw)
			assert.Equal(t, tt.wantScheme, got.Scheme)
			assert.Equal(t, tt.wantPath, got.Path)
			assert.Equal(t, tt.wantHostname, got.Hostname)
			assert.Equal(t, tt.wantPort, got.Port)
		})
	}
}

func TestParse_error(t *testing.T) {
	_, err := Parse("NOT\tA\nVALID URL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse remote URL")
}
