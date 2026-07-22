package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
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
	remoteURL, err := giturl.Parse("git@githubaccount1:example/repo.git")
	require.NoError(t, err)

	rid, err := f.ParseRepositoryPath(remoteURL.Path)
	require.NoError(t, err)

	assert.Equal(t, "example/repo", rid.String())
	assert.Equal(t,
		"https://github.com/example/repo/pull/123",
		rid.ChangeURL(&PR{Number: 123}))
}

func TestChangeURL(t *testing.T) {
	repoID := RepositoryID{
		url:   DefaultURL,
		owner: "example",
		name:  "repo",
	}

	got := repoID.ChangeURL(&PR{Number: 123})
	assert.Equal(t, "https://github.com/example/repo/pull/123", got)
}

func TestRepository_ComparisonURL(t *testing.T) {
	r := &Repository{
		owner: "example",
		repo:  "repo",
		forge: &Forge{Options: Options{URL: DefaultURL}},
	}

	t.Run("SameRepository", func(t *testing.T) {
		got := r.ComparisonURL(forge.ComparisonRequest{
			Base: "main",
			Head: "feat",
		})
		assert.Equal(t, "https://github.com/example/repo/compare/main...feat", got)
	})

	t.Run("Fork", func(t *testing.T) {
		got := r.ComparisonURL(forge.ComparisonRequest{
			Base: "main",
			Head: "feat#review",
			HeadRepository: &RepositoryID{
				url:   DefaultURL,
				owner: "fork",
				name:  "repo",
			},
		})
		assert.Equal(t,
			"https://github.com/example/repo/compare/main...fork:feat%23review",
			got)
	})
}
