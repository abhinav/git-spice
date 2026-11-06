package github

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git/giturl"
)

func TestOptions_Stacks(t *testing.T) {
	tests := []struct {
		name string
		give any
		want bool
	}{
		{name: "Default", want: true},
		{name: "Enabled", give: "true", want: true},
		{name: "Disabled", give: "false", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts Options
			parser, err := kong.New(
				&opts,
				kong.Resolvers(kong.ResolverFunc(func(
					_ *kong.Context,
					_ *kong.Path,
					flag *kong.Flag,
				) (any, error) {
					if flag.Tag.Get("config") == "forge.github.stacks" {
						return tt.give, nil
					}
					return nil, nil
				})),
			)
			require.NoError(t, err)

			_, err = parser.Parse(nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, opts.Stacks)
		})
	}
}

func TestURLs(t *testing.T) {
	tests := []struct {
		name string
		opts Options

		wantURL string
	}{
		{
			name:    "Default",
			wantURL: "https://github.com",
		},
		{
			name:    "ExplicitURL",
			opts:    Options{URL: "https://github.com"},
			wantURL: "https://github.com",
		},
		{
			name:    "CustomURL",
			opts:    Options{URL: "https://example.com"},
			wantURL: "https://example.com",
		},
		{
			name: "CustomBoth",
			opts: Options{
				URL:    "https://example.com",
				APIURL: "https://api.example.com",
			},
			wantURL: "https://example.com",
		},
		{
			name:    "InvalidURL",
			opts:    Options{URL: ":/"},
			wantURL: ":/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: tt.opts}
			d := Definition{Options: tt.opts}

			assert.Equal(t, tt.wantURL, f.URL())
			assert.Equal(t, tt.wantURL, d.BaseURL())
		})
	}
}

func TestForge_OpenRepository_endpoints(t *testing.T) {
	tests := []struct {
		name     string
		opts     Options
		wantGQL  string
		wantREST string
	}{
		{
			name:     "GitHub",
			wantGQL:  "https://api.github.com/graphql",
			wantREST: "https://api.github.com",
		},
		{
			name:     "ExplicitGitHub",
			opts:     Options{URL: "https://github.com"},
			wantGQL:  "https://api.github.com/graphql",
			wantREST: "https://api.github.com",
		},
		{
			name:     "MalformedWebURL",
			opts:     Options{URL: ":/"},
			wantGQL:  "https://api.github.com/graphql",
			wantREST: "https://api.github.com",
		},
		{
			name:     "EnterpriseCloud",
			opts:     Options{APIURL: "https://api.octocorp.ghe.com"},
			wantGQL:  "https://api.octocorp.ghe.com/graphql",
			wantREST: "https://api.octocorp.ghe.com",
		},
		{
			name:     "EnterpriseCloudMixedCasePort",
			opts:     Options{APIURL: "https://API.OctoCorp.GHE.COM:443"},
			wantGQL:  "https://API.OctoCorp.GHE.COM:443/graphql",
			wantREST: "https://API.OctoCorp.GHE.COM:443",
		},
		{
			name:     "Enterprise",
			opts:     Options{URL: "https://github.example.com"},
			wantGQL:  "https://github.example.com/api/graphql",
			wantREST: "https://github.example.com/api/v3",
		},
		{
			name:     "WebPrefix",
			opts:     Options{URL: "https://github.example.com/service"},
			wantGQL:  "https://github.example.com/service/api/graphql",
			wantREST: "https://github.example.com/service/api/v3",
		},
		{
			name: "CustomAPI",
			opts: Options{
				URL:    "https://github.example.com",
				APIURL: "https://proxy.example.com/github",
			},
			wantGQL:  "https://proxy.example.com/github/graphql",
			wantREST: "https://proxy.example.com/github/v3",
		},
		{
			name:     "EnterpriseCloudWebHost",
			opts:     Options{URL: "https://octocorp.ghe.com"},
			wantGQL:  "https://octocorp.ghe.com/api/graphql",
			wantREST: "https://octocorp.ghe.com/api/v3",
		},
		{
			name:     "EnterpriseSimilarCloudHost",
			opts:     Options{APIURL: "https://api.octocorp.ghe.com.example.net/api"},
			wantGQL:  "https://api.octocorp.ghe.com.example.net/api/graphql",
			wantREST: "https://api.octocorp.ghe.com.example.net/api/v3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stackURL := tt.wantREST + "/repos/octo/hello/stacks"
			var requests []string
			f := Forge{
				Options: tt.opts,
				HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					requests = append(requests, r.URL.String())
					assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
					status, body := http.StatusOK, "[]"
					switch r.URL.String() {
					case tt.wantGQL:
						assert.Equal(t, http.MethodPost, r.Method)
						body = `{"data":{"repository":{"id":"R_123"}}}`
					case stackURL:
						assert.Equal(t, http.MethodGet, r.Method)
					default:
						status, body = http.StatusNotFound, `{"message":"Not Found"}`
					}
					return &http.Response{
						StatusCode: status,
						Body:       io.NopCloser(strings.NewReader(body)),
					}, nil
				})},
			}
			f.Options.Stacks = true
			rid, err := f.ParseRepositoryPath("octo/hello")
			require.NoError(t, err)
			repo, err := f.OpenRepository(t.Context(), &AuthenticationToken{AccessToken: "token"}, rid)
			require.NoError(t, err)

			_, err = repo.(forge.StackRepository).PlanMergeRanges(t.Context(), nil)
			assert.NoError(t, err)
			assert.Equal(t, []string{tt.wantGQL, stackURL}, requests)
		})
	}
}

func TestDefinition_New_invalidAPIURL(t *testing.T) {
	d := Definition{Options: Options{
		URL:    "https://github.example.com",
		APIURL: ":/",
	}}
	assert.Equal(t, "https://github.example.com", d.BaseURL())
	remoteURL, err := giturl.Parse("https://github.example.com/octo/hello.git")
	require.NoError(t, err)
	f, err := d.New(remoteURL, &forge.NewOptions{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("unexpected HTTP request")
			return nil, nil
		})},
	})
	require.NoError(t, err)

	ep, err := f.(*Forge).oauth2Endpoint()
	require.NoError(t, err)
	assert.Equal(t, "https://github.example.com/login/oauth/access_token", ep.TokenURL)

	rid, err := f.ParseRepositoryPath("octo/hello")
	require.NoError(t, err)
	_, err = f.OpenRepository(t.Context(), &AuthenticationToken{AccessToken: "token"}, rid)
	assert.ErrorContains(t, err, "parse API URL")
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
		url:   "https://github.com",
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
		forge: &Forge{Options: Options{URL: "https://github.com"}},
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
				url:   "https://github.com",
				owner: "fork",
				name:  "repo",
			},
		})
		assert.Equal(t,
			"https://github.com/example/repo/compare/main...fork:feat%23review",
			got)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
