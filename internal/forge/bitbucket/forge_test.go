package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestForge_ID(t *testing.T) {
	f := &Forge{}
	assert.Equal(t, "bitbucket", f.ID())
}

func TestForge_URL(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		want    string
	}{
		{
			name:    "Default",
			options: Options{},
			want:    "https://bitbucket.org",
		},
		{
			name: "CustomURL",
			options: Options{
				URL: "https://bitbucket.example.com",
			},
			want: "https://bitbucket.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{Options: tt.options}
			assert.Equal(t, tt.want, f.URL())
		})
	}
}

func TestForge_APIURL(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		want    string
	}{
		{
			name:    "Default",
			options: Options{},
			want:    "https://api.bitbucket.org/2.0",
		},
		{
			name: "CustomAPIURL",
			options: Options{
				APIURL: "https://api.bitbucket.example.com/2.0",
			},
			want: "https://api.bitbucket.example.com/2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{Options: tt.options}
			assert.Equal(t, tt.want, f.APIURL())
		})
	}
}

func TestForge_ParseRepositoryPath(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      string
	}{
		{
			name:      "HTTPS",
			remoteURL: "https://bitbucket.org/workspace/repo.git",
			want:      "workspace/repo",
		},
		{
			name:      "HTTPSNoGit",
			remoteURL: "https://bitbucket.org/workspace/repo",
			want:      "workspace/repo",
		},
		{
			name:      "SSH",
			remoteURL: "git@bitbucket.org:workspace/repo.git",
			want:      "workspace/repo",
		},
		{
			name:      "SSHNoGit",
			remoteURL: "git@bitbucket.org:workspace/repo",
			want:      "workspace/repo",
		},
		{
			name:      "HTTPSWithPort443",
			remoteURL: "https://bitbucket.org:443/workspace/repo.git",
			want:      "workspace/repo",
		},
		{
			name:      "GitProtocol",
			remoteURL: "git://bitbucket.org/workspace/repo.git",
			want:      "workspace/repo",
		},
		{
			name:      "GitSSHProtocol",
			remoteURL: "git+ssh://git@bitbucket.org/workspace/repo.git",
			want:      "workspace/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{}
			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			rid, err := f.ParseRepositoryPath(remoteURL.Path)
			require.NoError(t, err)

			assert.Equal(t, tt.want, rid.String())
		})
	}
}

func TestForge_ParseRepositoryPath_errors(t *testing.T) {
	f := &Forge{}
	_, err := f.ParseRepositoryPath("/workspace")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsupportedURL)
}

func TestForge_ParseRepositoryPath_CustomURL(t *testing.T) {
	f := &Forge{
		Options: Options{
			URL: "https://bitbucket.example.com",
		},
	}

	tests := []struct {
		name      string
		remoteURL string
		want      string
	}{
		{
			name:      "HTTPS",
			remoteURL: "https://bitbucket.example.com/workspace/repo.git",
			want:      "workspace/repo",
		},
		{
			name:      "SSH",
			remoteURL: "git@bitbucket.example.com:workspace/repo.git",
			want:      "workspace/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			rid, err := f.ParseRepositoryPath(remoteURL.Path)
			require.NoError(t, err)

			assert.Equal(t, tt.want, rid.String())
		})
	}
}

func TestForge_ParseRepositoryPath_knownForge(t *testing.T) {
	f := &Forge{}
	remoteURL, err := giturl.Parse("git@bitbucket-alias:workspace/repo.git")
	require.NoError(t, err)

	rid, err := f.ParseRepositoryPath(remoteURL.Path)
	require.NoError(t, err)

	assert.Equal(t, "workspace/repo", rid.String())
	assert.Equal(t,
		"https://bitbucket.org/workspace/repo/pull-requests/42",
		rid.ChangeURL(&PR{Number: 42}))
}

func TestRepositoryID_ChangeURL(t *testing.T) {
	rid := &RepositoryID{
		url:       "https://bitbucket.org",
		workspace: "myworkspace",
		name:      "myrepo",
	}

	pr := &PR{Number: 42}
	got := rid.ChangeURL(pr)

	assert.Equal(t, "https://bitbucket.org/myworkspace/myrepo/pull-requests/42", got)
}

func TestForge_ChangeTemplatePaths(t *testing.T) {
	f := &Forge{}
	paths := f.ChangeTemplatePaths()

	assert.NotEmpty(t, paths)
	assert.Contains(t, paths, "PULL_REQUEST_TEMPLATE.md")
}
