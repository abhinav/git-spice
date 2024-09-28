package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLs(t *testing.T) {
	tests := []struct {
		name string
		opts Options

		wantURL    string
		wantAPIURL string
	}{
		{
			name:       "Default",
			wantURL:    DefaultURL,
			wantAPIURL: DefaultAPIURL,
		},
		{
			name:       "ExplicitURL",
			opts:       Options{URL: DefaultURL},
			wantURL:    DefaultURL,
			wantAPIURL: DefaultAPIURL,
		},
		{
			name:       "CustomURL",
			opts:       Options{URL: "https://example.com"},
			wantURL:    "https://example.com",
			wantAPIURL: "https://example.com/api",
		},
		{
			name: "CustomBoth",
			opts: Options{
				URL:    "https://example.com",
				APIURL: "https://api.example.com",
			},
			wantURL:    "https://example.com",
			wantAPIURL: "https://api.example.com",
		},
		{
			name:       "InvalidURL",
			opts:       Options{URL: ":/"},
			wantURL:    ":/",
			wantAPIURL: DefaultAPIURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: tt.opts}

			assert.Equal(t, tt.wantURL, f.URL())
			assert.Equal(t, tt.wantAPIURL, f.APIURL())
		})
	}
}

func TestExtractRepoInfo(t *testing.T) {
	tests := []struct {
		name      string
		give      string
		githubURL string

		wantOwner string
		wantRepo  string
	}{
		{
			name:      "https",
			give:      "https://github.com/example/repo",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "ssh",
			give:      "git@github.com:example/repo",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "ssh with git protocol",
			give:      "ssh://git@github.com/example/repo",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "https/trailing slash",
			give:      "https://github.com/example/repo/",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "ssh/.git",
			give:      "git@github.com:example/repo.git",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "https/.git/trailing slash",
			give:      "https://github.com/example/repo.git/",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "https/custom URL",
			give:      "https://example.com/example/repo",
			githubURL: "https://example.com",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			// https://github.com/abhinav/git-spice/issues/425
			name:      "ssh protocol with port",
			give:      "ssh://git@ssh.github.com:443/mycompany/myrepo.git",
			wantOwner: "mycompany",
			wantRepo:  "myrepo",
		},
		{
			name:      "ssh protocol with custom port",
			githubURL: "ssh://git@ssh.mygithub.example.com:1443",
			give:      "ssh://git@ssh.mygithub.example.com:1443/mycompany/myrepo",
			wantOwner: "mycompany",
			wantRepo:  "myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.githubURL}}
			owner, repo, err := extractRepoInfo(f.URL(), tt.give)
			require.NoError(t, err)

			assert.Equal(t, tt.wantOwner, owner, "owner")
			assert.Equal(t, tt.wantRepo, repo, "repo")
		})
	}
}

func TestExtractRepoInfoErrors(t *testing.T) {
	tests := []struct {
		name      string
		give      string
		githubURL string

		wantErr []string
	}{
		{
			name:      "bad github URL",
			give:      "https://github.com/example/repo",
			githubURL: "NOT\tA\nVALID URL",
			wantErr:   []string{"bad base URL"},
		},
		{
			name:    "bad remote URL",
			give:    "NOT\tA\nVALID URL",
			wantErr: []string{"parse remote URL"},
		},
		{
			name: "host mismatch",
			give: "https://example.com/example/repo",
			wantErr: []string{
				"not a GitHub URL",
				`expected host "github.com"`,
			},
		},
		{
			name:    "no owner",
			give:    "https://github.com/repo",
			wantErr: []string{"does not contain a GitHub repository"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.githubURL}}
			_, _, err := extractRepoInfo(f.URL(), tt.give)
			require.Error(t, err)

			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}
