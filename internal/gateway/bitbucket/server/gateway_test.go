package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

func TestNew_requiresURL(t *testing.T) {
	// Without an instance URL there is no instance to talk to.
	_, err := New(
		"", "",
		"KEY", "repo", false,
		silog.Nop(),
		&Token{AccessToken: "tok"},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no Bitbucket Data Center URL configured")
}

func TestNew_nilToken(t *testing.T) {
	_, err := New(
		"https://bitbucket.example.com/rest/api/1.0",
		"https://bitbucket.example.com",
		"ENG", "warp-core", false,
		silog.Nop(),
		nil,
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "nil authentication token")
}

func TestGateway_Product(t *testing.T) {
	gw := newTestServerGateway(t,
		"https://bitbucket.example.com",
		&serverRepositoryID{
			url:        "https://bitbucket.example.com",
			projectKey: "ENG",
			slug:       "warp-core",
		},
		silog.Nop(),
	)

	assert.Equal(t, "Bitbucket Data Center", gw.Product())
}

func TestGateway_ChangeURL(t *testing.T) {
	t.Run("Project", func(t *testing.T) {
		gw := newTestServerGateway(t,
			"https://bitbucket.example.com",
			&serverRepositoryID{
				url:        "https://bitbucket.example.com",
				projectKey: "KOLIBRI",
				slug:       "kolibri-maklerpost",
			},
			silog.Nop(),
		)
		assert.Equal(t,
			"https://bitbucket.example.com/projects/KOLIBRI/repos/kolibri-maklerpost/pull-requests/42/overview",
			gw.ChangeURL(42))
	})

	t.Run("Personal", func(t *testing.T) {
		gw := newTestServerGateway(t,
			"https://bitbucket.example.com",
			&serverRepositoryID{
				url:        "https://bitbucket.example.com",
				projectKey: "user",
				slug:       "repo",
				personal:   true,
			},
			silog.Nop(),
		)
		assert.Equal(t,
			"https://bitbucket.example.com/users/user/repos/repo/pull-requests/7/overview",
			gw.ChangeURL(7))
	})
}

func TestGateway_ChangeTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		// Omitting "at" makes the server use the default branch.
		assert.Empty(t, r.URL.Query().Get("at"))

		if r.URL.Path == "/rest/api/1.0/projects/ENG/repos/warp-core/raw/PULL_REQUEST_TEMPLATE.md" {
			_, err := w.Write([]byte("## Summary\n"))
			assert.NoError(t, err)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: "ENG",
		slug:       "warp-core",
	}, silog.Nop())

	body, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.NoError(t, err)
	assert.Equal(t, "## Summary\n", body)

	// A missing template reports forge.ErrNotFound
	// so the caller can skip the path.
	_, err = gw.ChangeTemplate(t.Context(), ".bitbucket/pull_request_template.md")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_ChangeTemplate_personalProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/1.0/projects/~jcaptain/repos/dotfiles/raw/PULL_REQUEST_TEMPLATE.md" {
			_, err := w.Write([]byte("personal template\n"))
			assert.NoError(t, err)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: "jcaptain",
		slug:       "dotfiles",
		personal:   true,
	}, silog.Nop())

	body, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.NoError(t, err)
	assert.Equal(t, "personal template\n", body)
}

func TestGateway_ChangeTemplate_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: "ENG",
		slug:       "warp-core",
	}, silog.Nop())

	// A server failure is not a missing template.
	_, err := gw.ChangeTemplate(t.Context(), "PULL_REQUEST_TEMPLATE.md")
	require.Error(t, err)
	assert.NotErrorIs(t, err, forge.ErrNotFound)
}

func TestGateway_ListCommitChecks(t *testing.T) {
	tests := []struct {
		name   string
		states []string
		want   []forge.ChangeCheck
	}{
		{"Empty", nil, []forge.ChangeCheck{}},
		{
			"Successful",
			[]string{"SUCCESSFUL"},
			[]forge.ChangeCheck{
				{Name: "build-0", State: forge.ChangeCheckPassed},
			},
		},
		{
			"InProgress",
			[]string{"SUCCESSFUL", "INPROGRESS"},
			[]forge.ChangeCheck{
				{Name: "build-0", State: forge.ChangeCheckPassed},
				{Name: "build-1", State: forge.ChangeCheckPending},
			},
		},
		{
			"Failed",
			[]string{"SUCCESSFUL", "INPROGRESS", "FAILED"},
			[]forge.ChangeCheck{
				{Name: "build-0", State: forge.ChangeCheckPassed},
				{Name: "build-1", State: forge.ChangeCheckPending},
				{Name: "build-2", State: forge.ChangeCheckFailed},
			},
		},
		// An unknown/future state (e.g. CANCELLED or STOPPED) must not be
		// reported as passing.
		{
			"Unknown",
			[]string{"CANCELLED"},
			[]forge.ChangeCheck{
				{Name: "build-0", State: forge.ChangeCheckFailed},
			},
		},
		{
			"UnknownBesideSuccessful",
			[]string{"SUCCESSFUL", "CANCELLED"},
			[]forge.ChangeCheck{
				{Name: "build-0", State: forge.ChangeCheckPassed},
				{Name: "build-1", State: forge.ChangeCheckFailed},
			},
		},
	}

	const sha = "feedface"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, buildStatusPath(sha), r.URL.Path)
				values := make([]map[string]any, 0, len(tt.states))
				for i, s := range tt.states {
					values = append(values, map[string]any{
						"key":   "build-" + strconv.Itoa(i),
						"state": s,
					})
				}
				gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
					"isLastPage": true,
					"values":     values,
				})
			}))
			defer srv.Close()

			gw := newOpsTestServerGateway(t, srv)
			got, err := gw.ListCommitChecks(t.Context(), git.Hash(sha))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_ListCommitChecks_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.ListCommitChecks(t.Context(), git.Hash("feedface"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "list build statuses")
}

func TestGateway_draftSupport(t *testing.T) {
	tests := []struct {
		name string

		// props is the /application-properties body, or nil to make the
		// endpoint fail with a 500.
		props map[string]any

		wantSupported bool
		wantKnown     bool
		wantVersion   string
	}{
		{
			name:          "supported/version",
			props:         map[string]any{"version": "9.4.0"},
			wantSupported: true,
			wantKnown:     true,
			wantVersion:   "9.4.0",
		},
		{
			name:          "supported/exactThreshold",
			props:         map[string]any{"version": "8.18.0"},
			wantSupported: true,
			wantKnown:     true,
			wantVersion:   "8.18.0",
		},
		{
			name:          "unsupported/justBelow",
			props:         map[string]any{"version": "8.17.9"},
			wantSupported: false,
			wantKnown:     true,
			wantVersion:   "8.17.9",
		},
		{
			name:          "supported/buildNumberFallback",
			props:         map[string]any{"version": "weird", "buildNumber": "8018000"},
			wantSupported: true,
			wantKnown:     true,
			wantVersion:   "weird",
		},
		{
			name:          "unsupported/buildNumberFallback",
			props:         map[string]any{"version": "weird", "buildNumber": "8017000"},
			wantSupported: false,
			wantKnown:     true,
			wantVersion:   "weird",
		},
		{
			// Neither a valid semver version nor a usable build number, so
			// support cannot be determined: known is false.
			name:          "unknown/invalidVersionAndBuildNumber",
			props:         map[string]any{"version": "weird", "buildNumber": "0"},
			wantSupported: false,
			wantKnown:     false,
			wantVersion:   "weird",
		},
		{
			name:          "unknown/endpointError",
			props:         nil,
			wantSupported: false,
			wantKnown:     false,
			wantVersion:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/rest/api/1.0/application-properties" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
					return
				}
				if tt.props == nil {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				gatewayWriteJSON(t, w, http.StatusOK, tt.props)
			}))
			defer srv.Close()

			gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
				url:        srv.URL,
				projectKey: "ENG",
				slug:       "warp-core",
			}, silog.Nop())

			supported, known, version := gw.draftSupport(t.Context())
			assert.Equal(t, tt.wantSupported, supported, "supported")
			assert.Equal(t, tt.wantKnown, known, "known")
			assert.Equal(t, tt.wantVersion, version, "version")
		})
	}
}

func TestGateway_draftSupport_memoized(t *testing.T) {
	var probes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/rest/api/1.0/application-properties", r.URL.Path)
		probes++
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})
	}))
	defer srv.Close()

	gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: "ENG",
		slug:       "warp-core",
	}, silog.Nop())

	for range 2 {
		supported, known, version := gw.draftSupport(t.Context())
		assert.True(t, supported, "supported")
		assert.True(t, known, "known")
		assert.Equal(t, "9.4.0", version, "version")
	}

	// A successful probe is memoized across calls.
	assert.Equal(t, 1, probes)
}

// newTestServerGateway builds a Gateway against the given test server
// URL via the production constructor, so the thin client is wired exactly
// as in production (BaseURL = URL + "/rest/api/1.0").
func newTestServerGateway(
	t *testing.T,
	serverURL string,
	rid *serverRepositoryID,
	log *silog.Logger,
) *Gateway {
	t.Helper()

	gw, err := New(
		serverURL+"/rest/api/1.0", serverURL,
		rid.projectKey, rid.slug, rid.personal,
		log,
		&Token{AccessToken: "test-token"},
	)
	require.NoError(t, err)
	return gw
}

// newOpsTestServerGateway builds a Gateway for the shared
// ENG/warp-core test repository served by srv.
func newOpsTestServerGateway(t *testing.T, srv *httptest.Server) *Gateway {
	t.Helper()
	return newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: testProjectKey,
		slug:       testSlug,
	}, silog.Nop())
}

// The shared test repository that the server gateway tests talk to.
const (
	testProjectKey = "ENG"
	testSlug       = "warp-core"
)

// prListPath and prItemPath build the REST paths the server gateway
// hits for the test repository.
func prListPath() string {
	return "/rest/api/1.0/projects/" + testProjectKey + "/repos/" + testSlug + "/pull-requests"
}

func prItemPath(id int64) string {
	return prListPath() + "/" + strconv.FormatInt(id, 10)
}

// buildStatusPath is the REST path for a commit's build statuses.
func buildStatusPath(sha string) string {
	return "/rest/build-status/1.0/commits/" + sha
}

// gatewayWriteJSON writes a JSON response with the given status code.
func gatewayWriteJSON(t *testing.T, w http.ResponseWriter, code int, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}
