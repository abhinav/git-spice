package bitbucket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestForge_APIURL_server(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		want    string
	}{
		{
			name: "DerivedFromURL",
			options: Options{
				URL: "https://bitbucket.example.com",
			},
			want: "https://bitbucket.example.com/rest/api/1.0",
		},
		{
			name: "CustomAPIURL",
			options: Options{
				URL:    "https://bitbucket.example.com",
				APIURL: "https://bitbucket.example.com/custom/api",
			},
			want: "https://bitbucket.example.com/custom/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{Options: tt.options}
			assert.Equal(t, tt.want, f.APIURL())
		})
	}
}

func TestForge_ParseRepositoryPath_server(t *testing.T) {
	const baseURL = "https://bitbucket.example.com"

	tests := []struct {
		name      string
		remoteURL string

		wantProject  string
		wantSlug     string
		wantPersonal bool
		wantString   string
	}{
		{
			name:        "SSH",
			remoteURL:   "ssh://git@bitbucket.example.com/kolibri/kolibri-maklerpost.git",
			wantProject: "kolibri",
			wantSlug:    "kolibri-maklerpost",
			wantString:  "kolibri/kolibri-maklerpost",
		},
		{
			name:        "SCPStyleSSH",
			remoteURL:   "git@bitbucket.example.com:kolibri/kolibri-maklerpost.git",
			wantProject: "kolibri",
			wantSlug:    "kolibri-maklerpost",
			wantString:  "kolibri/kolibri-maklerpost",
		},
		{
			name:        "HTTPSWithSCMStripped",
			remoteURL:   "https://bitbucket.example.com/scm/KEY/repo.git",
			wantProject: "KEY", // case preserved verbatim
			wantSlug:    "repo",
			wantString:  "KEY/repo",
		},
		{
			name:        "HTTPSNoGitSuffix",
			remoteURL:   "https://bitbucket.example.com/scm/KEY/repo",
			wantProject: "KEY",
			wantSlug:    "repo",
			wantString:  "KEY/repo",
		},
		{
			name:        "HTTPSWithContextPath",
			remoteURL:   "https://bitbucket.example.com/bitbucket/scm/KEY/repo.git",
			wantProject: "KEY",
			wantSlug:    "repo",
			wantString:  "KEY/repo",
		},
		{
			name:         "PersonalSSH",
			remoteURL:    "ssh://git@bitbucket.example.com/~user/repo.git",
			wantProject:  "user",
			wantSlug:     "repo",
			wantPersonal: true,
			wantString:   "~user/repo",
		},
		{
			name:         "PersonalHTTPS",
			remoteURL:    "https://bitbucket.example.com/scm/~user/repo.git",
			wantProject:  "user",
			wantSlug:     "repo",
			wantPersonal: true,
			wantString:   "~user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Forge{Options: Options{URL: baseURL}}
			remoteURL, err := giturl.Parse(tt.remoteURL)
			require.NoError(t, err)

			id, err := f.ParseRepositoryPath(remoteURL.Path)
			require.NoError(t, err)

			rid, ok := id.(*serverRepositoryID)
			require.True(t, ok)

			assert.Equal(t, tt.wantProject, rid.projectKey, "projectKey")
			assert.Equal(t, tt.wantSlug, rid.slug, "slug")
			assert.Equal(t, tt.wantPersonal, rid.personal, "personal")
			assert.Equal(t, tt.wantString, rid.String(), "String")
		})
	}
}

func TestForge_ParseRepositoryPath_serverErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "OnlyProject", path: "/kolibri"},
		{name: "Empty", path: "/"},
		{name: "TrailingSlugSegments", path: "/KEY/repo/extra.git"},
		{name: "EmptyPersonalUser", path: "/~/repo.git"},
		{name: "SCMOnlyProject", path: "/scm/KEY"},
		{name: "SCMTrailingSlugSegments", path: "/ctx/scm/KEY/repo/extra.git"},
	}

	f := &Forge{Options: Options{URL: "https://bitbucket.example.com"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.ParseRepositoryPath(tt.path)
			require.Error(t, err)
			assert.ErrorIs(t, err, forge.ErrUnsupportedURL)
		})
	}
}

func TestServerRepositoryID_ChangeURL(t *testing.T) {
	t.Run("Project", func(t *testing.T) {
		rid := &serverRepositoryID{
			url:        "https://bitbucket.example.com",
			projectKey: "KOLIBRI",
			slug:       "kolibri-maklerpost",
		}
		assert.Equal(t,
			"https://bitbucket.example.com/projects/KOLIBRI/repos/kolibri-maklerpost/pull-requests/42/overview",
			rid.ChangeURL(&PR{Number: 42}))
	})

	t.Run("Personal", func(t *testing.T) {
		rid := &serverRepositoryID{
			url:        "https://bitbucket.example.com",
			projectKey: "user",
			slug:       "repo",
			personal:   true,
		}
		assert.Equal(t,
			"https://bitbucket.example.com/users/user/repos/repo/pull-requests/7/overview",
			rid.ChangeURL(&PR{Number: 7}))
	})
}

func TestForge_OpenRepository_requiresURL(t *testing.T) {
	f := &Forge{Options: Options{Kind: KindDataCenter}}
	_, err := f.OpenRepository(
		t.Context(),
		&AuthenticationToken{AccessToken: "tok"},
		&serverRepositoryID{url: "", projectKey: "KEY", slug: "repo"},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no Bitbucket Data Center URL configured")
}
