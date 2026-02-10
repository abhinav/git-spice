package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
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

func TestForge_ParseRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      string
		wantErr   error
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
		{
			name:      "WrongHost",
			remoteURL: "https://github.com/owner/repo.git",
			wantErr:   forge.ErrUnsupportedURL,
		},
		{
			name:      "NoRepo",
			remoteURL: "https://bitbucket.org/workspace",
			wantErr:   forge.ErrUnsupportedURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{}
			rid, err := f.ParseRemoteURL(tt.remoteURL)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, rid.String())
		})
	}
}

func TestForge_ParseRemoteURL_CustomURL(t *testing.T) {
	f := &Forge{
		Options: Options{
			URL: "https://bitbucket.example.com",
		},
	}

	tests := []struct {
		name      string
		remoteURL string
		want      string
		wantErr   error
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
		{
			name:      "WrongHost",
			remoteURL: "https://bitbucket.org/workspace/repo.git",
			wantErr:   forge.ErrUnsupportedURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rid, err := f.ParseRemoteURL(tt.remoteURL)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, rid.String())
		})
	}
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
