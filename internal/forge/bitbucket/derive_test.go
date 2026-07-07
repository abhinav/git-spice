package bitbucket

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestDefinition_New(t *testing.T) {
	tests := []struct {
		name      string
		presetURL string
		remoteURL string
		wantURL   string
	}{
		{
			name:      "HTTPS",
			remoteURL: "https://git.corp.com/scm/PROJ/repo.git",
			wantURL:   "https://git.corp.com",
		},
		{
			name:      "HTTPSPortAndContextPath",
			remoteURL: "https://git.corp.com:8443/bitbucket/scm/PROJ/repo.git",
			wantURL:   "https://git.corp.com:8443/bitbucket",
		},
		{
			name:      "HTTPKeepsScheme",
			remoteURL: "http://git.corp.com/scm/PROJ/repo.git",
			wantURL:   "http://git.corp.com",
		},
		{
			name:      "SSHDropsPort",
			remoteURL: "ssh://git@git.corp.com:7999/PROJ/repo.git",
			wantURL:   "https://git.corp.com",
		},
		{
			name:      "PersonalRepository",
			remoteURL: "https://git.corp.com/scm/~user/repo.git",
			wantURL:   "https://git.corp.com",
		},
		{
			name:      "CloudSCPStyle",
			remoteURL: "git@bitbucket.org:ws/repo.git",
			wantURL:   "",
		},
		{
			name:      "CloudSubdomain",
			remoteURL: "https://x.bitbucket.org/ws/repo.git",
			wantURL:   "",
		},
		{
			name:      "ExplicitURLWinsForMatchingRemote",
			presetURL: "https://git.corp.com",
			remoteURL: "https://git.corp.com/scm/PROJ/repo.git",
			wantURL:   "https://git.corp.com",
		},
		{
			name:      "HostAlias",
			remoteURL: "gh-work:org/repo.git",
			wantURL:   "https://gh-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			d := &Definition{Options: Options{URL: tt.presetURL}}
			got, err := d.New(remoteURL)
			require.NoError(t, err)

			assert.Equal(t, tt.presetURL, d.Options.URL)
			assert.Equal(t, cmp.Or(tt.wantURL, DefaultURL), got.(*Forge).URL())
		})
	}
}
