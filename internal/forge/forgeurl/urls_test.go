package forgeurl

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasGitProtocol(t *testing.T) {
	tests := []struct {
		name string
		give string
		want bool
	}{
		{"HTTPS", "https://github.com/owner/repo", true},
		{"HTTP", "http://github.com/owner/repo", true},
		{"SSH protocol", "ssh://git@github.com/owner/repo", true},
		{"Git protocol", "git://github.com/owner/repo.git", true},
		{"Git+SSH", "git+ssh://git@github.com/owner/repo", true},
		{"Git+HTTPS", "git+https://github.com/owner/repo", true},
		{"Git+HTTP", "git+http://github.com/owner/repo", true},
		{"SCP-style SSH", "git@github.com:owner/repo", false},
		{"Plain path", "/path/to/repo", false},
		{"Empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasGitProtocol(tt.give)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		give     string
		wantHost string
		wantPath string
	}{
		{
			name:     "HTTPS",
			give:     "https://github.com/owner/repo",
			wantHost: "github.com",
			wantPath: "/owner/repo",
		},
		{
			name:     "SSH protocol",
			give:     "ssh://git@github.com/owner/repo",
			wantHost: "github.com",
			wantPath: "/owner/repo",
		},
		{
			name:     "SCP-style SSH normalized",
			give:     "git@github.com:owner/repo",
			wantHost: "github.com",
			wantPath: "/owner/repo",
		},
		{
			name:     "SSH with port",
			give:     "ssh://git@ssh.github.com:443/owner/repo",
			wantHost: "ssh.github.com:443",
			wantPath: "/owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.give)
			require.NoError(t, err)

			assert.Equal(t, tt.wantHost, got.Host)
			assert.Equal(t, tt.wantPath, got.Path)
		})
	}
}

func TestParse_error(t *testing.T) {
	_, err := Parse("NOT\tA\nVALID URL")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse remote URL")
}

func TestStripDefaultPort(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		remoteHost string
		wantHost   string
	}{
		{
			name:       "strip 443",
			baseURL:    "https://github.com",
			remoteHost: "github.com:443",
			wantHost:   "github.com",
		},
		{
			name:       "strip 80",
			baseURL:    "http://github.com",
			remoteHost: "github.com:80",
			wantHost:   "github.com",
		},
		{
			name:       "keep custom port",
			baseURL:    "https://github.com",
			remoteHost: "github.com:8443",
			wantHost:   "github.com:8443",
		},
		{
			name:       "base has port",
			baseURL:    "https://github.com:443",
			remoteHost: "github.com:443",
			wantHost:   "github.com:443",
		},
		{
			name:       "no port to strip",
			baseURL:    "https://github.com",
			remoteHost: "github.com",
			wantHost:   "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, err := url.Parse(tt.baseURL)
			require.NoError(t, err)

			remoteURL := &url.URL{Host: tt.remoteHost}
			StripDefaultPort(baseURL, remoteURL)

			assert.Equal(t, tt.wantHost, remoteURL.Host)
		})
	}
}

func TestMatchesHost(t *testing.T) {
	tests := []struct {
		name       string
		baseHost   string
		remoteHost string
		want       bool
	}{
		{
			name:       "exact match",
			baseHost:   "github.com",
			remoteHost: "github.com",
			want:       true,
		},
		{
			name:       "subdomain match",
			baseHost:   "github.com",
			remoteHost: "ssh.github.com",
			want:       true,
		},
		{
			name:       "no match",
			baseHost:   "github.com",
			remoteHost: "gitlab.com",
			want:       false,
		},
		{
			name:       "partial suffix not a match",
			baseHost:   "github.com",
			remoteHost: "notgithub.com",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL := &url.URL{Host: tt.baseHost}
			remoteURL := &url.URL{Host: tt.remoteHost}

			got := MatchesHost(baseURL, remoteURL)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractPath(t *testing.T) {
	tests := []struct {
		name      string
		give      string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "simple",
			give:      "/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "with .git suffix",
			give:      "/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "trailing slash",
			give:      "/owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "both suffix and slash",
			give:      "/owner/repo.git/",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "no leading slash",
			give:      "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:   "no repo component",
			give:   "/owner",
			wantOK: false,
		},
		{
			name:   "empty",
			give:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := ExtractPath(tt.give)

			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantOwner, owner)
				assert.Equal(t, tt.wantRepo, repo)
			}
		})
	}
}
