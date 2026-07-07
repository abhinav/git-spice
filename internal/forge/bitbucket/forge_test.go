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
		remote  string
		want    string
	}{
		{
			name:    "Default",
			options: Options{},
			remote:  "https://bitbucket.org/workspace/repo.git",
			want:    "https://bitbucket.org",
		},
		{
			name: "CustomURL",
			options: Options{
				URL: "https://bitbucket.example.com",
			},
			remote: "https://bitbucket.example.com/scm/KEY/repo.git",
			want:   "https://bitbucket.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newForgeForTest(t, tt.options, tt.remote)
			assert.Equal(t, tt.want, f.URL())
		})
	}
}

func TestForge_APIURL(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		remote  string
		want    string
	}{
		{
			name:    "Default",
			options: Options{},
			remote:  "https://bitbucket.org/workspace/repo.git",
			want:    "https://api.bitbucket.org/2.0",
		},
		{
			name: "CustomAPIURL",
			options: Options{
				APIURL: "https://api.bitbucket.example.com/2.0",
			},
			remote: "https://bitbucket.org/workspace/repo.git",
			want:   "https://api.bitbucket.example.com/2.0",
		},
		{
			name: "CustomURLDataCenter",
			options: Options{
				URL: "https://bitbucket.example.com",
			},
			remote: "https://bitbucket.example.com/scm/KEY/repo.git",
			want:   "https://bitbucket.example.com/rest/api/1.0",
		},
		{
			name: "CustomURLCloudKind",
			options: Options{
				URL:  "https://bitbucket.example.com",
				Kind: KindCloud,
			},
			remote: "https://bitbucket.example.com/workspace/repo.git",
			want:   "https://api.bitbucket.org/2.0",
		},
		{
			name: "CustomAPIURLDataCenterKind",
			options: Options{
				URL:    "https://bitbucket.example.com",
				APIURL: "https://bitbucket.example.com/custom/api",
				Kind:   KindDataCenter,
			},
			remote: "https://bitbucket.example.com/scm/KEY/repo.git",
			want:   "https://bitbucket.example.com/custom/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newForgeForTest(t, tt.options, tt.remote)
			assert.Equal(t, tt.want, f.APIURL())
		})
	}
}

func TestDefinition_New_kind(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		remote  string
		want    Kind
	}{
		{
			name:   "Default",
			remote: "https://bitbucket.org/workspace/repo.git",
			want:   KindCloud,
		},
		{
			name:    "CloudURL",
			options: Options{URL: "https://bitbucket.org"},
			remote:  "https://bitbucket.org/workspace/repo.git",
			want:    KindCloud,
		},
		{
			name:    "CloudSubdomainURL",
			options: Options{URL: "https://sub.bitbucket.org"},
			remote:  "https://sub.bitbucket.org/workspace/repo.git",
			want:    KindCloud,
		},
		{
			name:    "CustomURL",
			options: Options{URL: "https://bitbucket.example.com"},
			remote:  "https://bitbucket.example.com/scm/KEY/repo.git",
			want:    KindDataCenter,
		},
		{
			name:    "ExplicitDataCenter",
			options: Options{Kind: KindDataCenter},
			remote:  "https://bitbucket.example.com/scm/KEY/repo.git",
			want:    KindDataCenter,
		},
		{
			name: "ExplicitCloudWithCustomURL",
			options: Options{
				URL:  "https://bitbucket.example.com",
				Kind: KindCloud,
			},
			remote: "https://bitbucket.example.com/workspace/repo.git",
			want:   KindCloud,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newForgeForTest(t, tt.options, tt.remote)
			assert.Equal(t, tt.want, f.kind)
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
			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)
			f := newForgeForTest(t, Options{}, tt.remoteURL)

			rid, err := f.ParseRepositoryPath(remoteURL.Path)
			require.NoError(t, err)

			assert.Equal(t, tt.want, rid.String())
		})
	}
}

func TestForge_ParseRepositoryPath_errors(t *testing.T) {
	f := newForgeForTest(t, Options{}, "https://bitbucket.org/workspace/repo.git")
	_, err := f.ParseRepositoryPath("/workspace")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsupportedURL)
}

func TestForge_ParseRepositoryPath_CustomURL(t *testing.T) {
	t.Run("DataCenter", func(t *testing.T) {
		f := newForgeForTest(t,
			Options{URL: "https://bitbucket.example.com"},
			"https://bitbucket.example.com/scm/proj/repo.git")

		tests := []struct {
			name      string
			remoteURL string
			want      string
		}{
			{
				name:      "HTTPS",
				remoteURL: "https://bitbucket.example.com/scm/proj/repo.git",
				want:      "proj/repo",
			},
			{
				name:      "HTTPSContextPath",
				remoteURL: "https://bitbucket.example.com/bitbucket/scm/proj/repo.git",
				want:      "proj/repo",
			},
			{
				name:      "SSH",
				remoteURL: "ssh://git@bitbucket.example.com:7999/proj/repo.git",
				want:      "proj/repo",
			},
			{
				name:      "Personal",
				remoteURL: "https://bitbucket.example.com/scm/~user/repo.git",
				want:      "~user/repo",
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
	})

	t.Run("Cloud", func(t *testing.T) {
		f := newForgeForTest(t,
			Options{
				URL:  "https://bitbucket.example.com",
				Kind: KindCloud,
			},
			"https://bitbucket.example.com/workspace/repo.git")

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
	})
}

func TestForge_ParseRepositoryPath_knownForge(t *testing.T) {
	remoteURL, err := giturl.Parse("git@bitbucket-alias:workspace/repo.git")
	require.NoError(t, err)
	f := newForgeForTest(t, Options{Kind: KindCloud}, "git@bitbucket-alias:workspace/repo.git")

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
		kind:      KindCloud,
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

func TestInferFromRemoteURL(t *testing.T) {
	t.Run("Cloud", func(t *testing.T) {
		var forges forge.Registry
		forges.Register(&Definition{})

		tests := []struct {
			name      string
			remoteURL string
		}{
			{
				name:      "HTTPS",
				remoteURL: "https://bitbucket.org/ws/repo",
			},
			{
				name:      "SCPStyleSSH",
				remoteURL: "git@bitbucket.org:ws/repo.git",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				remoteURL, err := giturl.Parse(tt.remoteURL)
				require.NoError(t, err)

				f, rid, ok := forge.InferFromRemoteURL(&forges, remoteURL)
				require.True(t, ok, "forge not found")

				assert.Equal(t, "bitbucket", f.ID())
				assert.IsType(t, (*RepositoryID)(nil), rid)
				assert.Equal(t, "ws/repo", rid.String())
			})
		}
	})

	t.Run("CustomURL", func(t *testing.T) {
		var forges forge.Registry
		forges.Register(&Definition{
			Options: Options{URL: "https://git.corp.com"},
		})

		tests := []struct {
			name      string
			remoteURL string
		}{
			{
				name:      "HTTPS",
				remoteURL: "https://git.corp.com/scm/PROJ/repo.git",
			},
			{
				name:      "SSHWithPort",
				remoteURL: "ssh://git@git.corp.com:7999/PROJ/repo.git",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				remoteURL, err := giturl.Parse(tt.remoteURL)
				require.NoError(t, err)

				f, rid, ok := forge.InferFromRemoteURL(&forges, remoteURL)
				require.True(t, ok, "forge not found")

				assert.Equal(t, "bitbucket", f.ID())
				assert.IsType(t, (*RepositoryID)(nil), rid)
				assert.Equal(t, "PROJ/repo", rid.String())
			})
		}

		t.Run("BitbucketOrgDoesNotMatch", func(t *testing.T) {
			remoteURL, err := giturl.Parse("git@bitbucket.org:ws/repo.git")
			require.NoError(t, err)

			_, _, ok := forge.InferFromRemoteURL(&forges, remoteURL)
			assert.False(t, ok, "unexpected forge match")
		})
	})
}

func newForgeForTest(t *testing.T, options Options, rawRemoteURL string) *Forge {
	t.Helper()

	remoteURL, err := giturl.Parse(rawRemoteURL)
	require.NoError(t, err)

	f, err := (&Definition{Options: options}).New(remoteURL)
	require.NoError(t, err)
	return f.(*Forge)
}
