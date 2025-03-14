package gitlab

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
			wantAPIURL: DefaultURL,
		},
		{
			name:       "ExplicitURL",
			opts:       Options{URL: DefaultURL},
			wantURL:    DefaultURL,
			wantAPIURL: DefaultURL,
		},
		{
			name:       "CustomURL",
			opts:       Options{URL: "https://example.com"},
			wantURL:    "https://example.com",
			wantAPIURL: "https://example.com",
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
			wantAPIURL: ":/",
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
		gitlabURL string

		wantOwner string
		wantRepo  string
	}{
		{
			name:      "https",
			give:      "https://gitlab.com/example/repo",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "ssh",
			give:      "git@gitlab.com:example/repo",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "ssh with git protocol",
			give:      "ssh://git@gitlab.com/example/repo",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "https/trailing slash",
			give:      "https://gitlab.com/example/repo/",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "ssh/.git",
			give:      "git@gitlab.com:example/repo.git",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "https/.git/trailing slash",
			give:      "https://gitlab.com/example/repo.git/",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			name:      "https/custom URL",
			give:      "https://example.com/example/repo",
			gitlabURL: "https://example.com",
			wantOwner: "example",
			wantRepo:  "repo",
		},
		{
			// https://github.com/abhinav/git-spice/issues/425
			name:      "ssh protocol with port",
			give:      "ssh://git@ssh.gitlab.com:443/mycompany/myrepo.git",
			wantOwner: "mycompany",
			wantRepo:  "myrepo",
		},
		{
			name:      "ssh protocol with custom port",
			gitlabURL: "ssh://git@ssh.mygitlab.example.com:1443",
			give:      "ssh://git@ssh.mygitlab.example.com:1443/mycompany/myrepo",
			wantOwner: "mycompany",
			wantRepo:  "myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.gitlabURL}}
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
		gitLabURL string

		wantErr []string
	}{
		{
			name:      "bad gitlab URL",
			give:      "https://gitlab.com/example/repo",
			gitLabURL: "NOT\tA\nVALID URL",
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
				"not a GitLab URL",
				`expected host "gitlab.com"`,
			},
		},
		{
			name:    "no owner",
			give:    "https://gitlab.com/repo",
			wantErr: []string{"does not contain a GitLab repository"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.gitLabURL}}
			_, _, err := extractRepoInfo(f.URL(), tt.give)
			require.Error(t, err)

			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestChangeURL(t *testing.T) {
	f := Forge{Options: Options{URL: "https://gitlab.com"}}

	t.Run("Valid", func(t *testing.T) {
		got, err := f.ChangeURL("https://gitlab.com/example/repo", &MR{Number: 42})
		require.NoError(t, err)
		assert.Equal(t, "https://gitlab.com/example/repo/-/merge_requests/42", got)
	})

	t.Run("Invalid", func(t *testing.T) {
		_, err := f.ChangeURL("https://notgitlab.com/example/repo", &MR{Number: 42})
		require.Error(t, err)
	})
}
