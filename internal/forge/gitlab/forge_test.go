package gitlab

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/giturl"
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
		{
			name:      "ssh protocol with arbitrary port on matching host",
			gitlabURL: "https://gitlab.example.com",
			give:      "ssh://git@gitlab.example.com:12051/mycompany/myrepo.git",
			wantOwner: "mycompany",
			wantRepo:  "myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.gitlabURL}}
			remoteURL, err := giturl.Parse(tt.give)
			require.NoError(t, err)

			rid, err := f.ParseRepositoryPath(remoteURL.Path)
			require.NoError(t, err)

			assert.Equal(t,
				tt.wantOwner+"/"+tt.wantRepo,
				rid.String(),
				"repository ID")
		})
	}
}

func TestExtractRepoInfoErrors(t *testing.T) {
	tests := []struct {
		name string
		give string
	}{
		{
			name: "no owner",
			give: "/repo",
		},
		{
			name: "empty",
			give: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{}
			_, err := f.ParseRepositoryPath(tt.give)
			require.Error(t, err)
		})
	}
}

func TestExtractRepoInfoErrors_badRemoteURL(t *testing.T) {
	_, err := giturl.Parse("NOT\tA\nVALID URL")
	require.Error(t, err)
	assert.ErrorContains(t, err, "parse remote URL")
}

func TestForge_ParseRepositoryPath_knownForge(t *testing.T) {
	f := Forge{}
	remoteURL, err := giturl.Parse("git@gitlab-ssh-alias:example/repo.git")
	require.NoError(t, err)

	rid, err := f.ParseRepositoryPath(remoteURL.Path)
	require.NoError(t, err)

	assert.Equal(t, "example/repo", rid.String())
	assert.Equal(t,
		"https://gitlab.com/example/repo/-/merge_requests/42",
		rid.ChangeURL(&MR{Number: 42}))
}

func TestChangeURL(t *testing.T) {
	repoID := RepositoryID{
		url:   DefaultURL,
		owner: "example",
		name:  "repo",
	}

	got := repoID.ChangeURL(&MR{Number: 42})
	assert.Equal(t, "https://gitlab.com/example/repo/-/merge_requests/42", got)
}
