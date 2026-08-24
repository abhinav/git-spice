package azuredevops

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
	"go.abhg.dev/gs/internal/xec/xectest"
	"go.uber.org/mock/gomock"
)

func TestForge_ID(t *testing.T) {
	f := Forge{}
	assert.Equal(t, "azuredevops", f.ID())
}

func TestForge_URL(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantURL string
	}{
		{
			name:    "Default",
			wantURL: DefaultURL,
		},
		{
			name:    "ExplicitURL",
			opts:    Options{URL: DefaultURL},
			wantURL: DefaultURL,
		},
		{
			name:    "CustomURL",
			opts:    Options{URL: "https://dev.azure.example.com"},
			wantURL: "https://dev.azure.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: tt.opts}
			assert.Equal(t, tt.wantURL, f.URL())
		})
	}
}

func TestExtractRepoInfo(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		give     string
		wantOrg  string
		wantProj string
		wantRepo string
	}{
		{
			name:     "HTTPS",
			give:     "https://dev.azure.com/myorg/myproject/_git/myrepo",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "HTTPS/TrailingSlash",
			give:     "https://dev.azure.com/myorg/myproject/_git/myrepo/",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "HTTPS/.git",
			give:     "https://dev.azure.com/myorg/myproject/_git/myrepo.git",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "SSH",
			give:     "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "SSH/.git",
			give:     "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo.git",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
		{
			name:     "CustomURL",
			baseURL:  "https://azuredevops.example.com",
			give:     "https://azuredevops.example.com/myorg/myproject/_git/myrepo",
			wantOrg:  "myorg",
			wantProj: "myproject",
			wantRepo: "myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.baseURL}}
			org, proj, repo, err := extractRepoInfo(f.URL(), tt.give)
			require.NoError(t, err)

			assert.Equal(t, tt.wantOrg, org, "organization")
			assert.Equal(t, tt.wantProj, proj, "project")
			assert.Equal(t, tt.wantRepo, repo, "repository")
		})
	}
}

func TestExtractRepoInfo_errors(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		give    string
		wantErr []string
	}{
		{
			name:    "BadBaseURL",
			baseURL: "NOT\tA\nVALID URL",
			give:    "https://dev.azure.com/myorg/myproject/_git/myrepo",
			wantErr: []string{"bad base URL"},
		},
		{
			name:    "BadRemoteURL",
			give:    "NOT\tA\nVALID URL",
			wantErr: []string{"parse remote URL"},
		},
		{
			name: "HostMismatch",
			give: "https://github.com/example/repo",
			wantErr: []string{
				"not an Azure DevOps URL",
				`expected host "dev.azure.com"`,
			},
		},
		{
			name:    "MissingGitSegment",
			give:    "https://dev.azure.com/myorg/myproject/myrepo",
			wantErr: []string{"invalid Azure DevOps URL path"},
		},
		{
			name:    "TooFewPathSegments",
			give:    "https://dev.azure.com/myorg/_git/myrepo",
			wantErr: []string{"invalid Azure DevOps URL path"},
		},
		{
			name:    "InvalidSSHFormat",
			give:    "git@ssh.dev.azure.com:v3/myorg/myproject",
			wantErr: []string{"invalid Azure DevOps SSH URL format"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Forge{Options: Options{URL: tt.baseURL}}
			_, _, _, err := extractRepoInfo(f.URL(), tt.give)
			require.Error(t, err)

			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestRepositoryID_String(t *testing.T) {
	rid := RepositoryID{
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	assert.Equal(t, "myorg/myproject/myrepo", rid.String())
}

func TestRepositoryID_ChangeURL(t *testing.T) {
	rid := RepositoryID{
		url:          DefaultURL,
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	got := rid.ChangeURL(&PR{Number: 123})
	assert.Equal(
		t,
		"https://dev.azure.com/myorg/myproject/_git/myrepo/pullrequest/123",
		got,
	)
}

func TestParseRemoteURL(t *testing.T) {
	f := Forge{}

	rid, err := f.ParseRemoteURL(
		"https://dev.azure.com/myorg/myproject/_git/myrepo",
	)
	require.NoError(t, err)

	azureRID := rid.(*RepositoryID)
	assert.Equal(t, "myorg", azureRID.organization)
	assert.Equal(t, "myproject", azureRID.project)
	assert.Equal(t, "myrepo", azureRID.repository)
}

func TestParseRemoteURL_error(t *testing.T) {
	f := Forge{}

	_, err := f.ParseRemoteURL("https://github.com/example/repo")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported URL")
}

func TestOpenRepository_refreshAzureCLIToken(t *testing.T) {
	// When the token is from Azure CLI,
	// OpenRepository should refresh it
	// by calling 'az account get-access-token'.

	// Capture the Authorization header sent to the server.
	var gotAuthHeaders []string
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuthHeaders = append(
				gotAuthHeaders, r.Header.Get("Authorization"),
			)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 0,
				"value": []any{},
			})
		}),
	)
	defer srv.Close()

	mockCtrl := gomock.NewController(t)
	execer := xectest.NewMockExecer(mockCtrl)

	// 'az account get-access-token' returns a fresh token.
	execer.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			_, _ = cmd.Stdout.Write([]byte("fresh-token\n"))
			return nil
		})

	old := _execLookPath
	_execLookPath = func(string) (string, error) {
		return "az", nil
	}
	defer func() { _execLookPath = old }()

	f := Forge{
		Options: Options{URL: srv.URL},
		Log:     silog.Nop(),
		Execer:  execer,
	}

	tok := &AuthenticationToken{
		AccessToken: "stale-token",
		AuthType:    AuthTypeAzureCLI,
	}
	rid := &RepositoryID{
		url:          srv.URL,
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	// OpenRepository will fail because the mock server
	// doesn't return real Azure DevOps responses,
	// but we can verify the token was refreshed.
	_, _ = f.OpenRepository(t.Context(), tok, rid)

	require.NotEmpty(t, gotAuthHeaders,
		"expected at least one request to the server")

	// The Azure DevOps SDK encodes PAT connections as
	// Basic auth with empty username: base64(":" + token).
	wantEncoded := base64.StdEncoding.EncodeToString(
		[]byte(":fresh-token"),
	)
	for _, hdr := range gotAuthHeaders {
		assert.Equal(t, "Basic "+wantEncoded, hdr,
			"server should receive the refreshed token")
	}
}

func TestOpenRepository_noRefreshForPAT(t *testing.T) {
	// When the token is a PAT, OpenRepository should use it
	// directly without attempting to refresh.

	var gotAuthHeaders []string
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuthHeaders = append(
				gotAuthHeaders, r.Header.Get("Authorization"),
			)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 0,
				"value": []any{},
			})
		}),
	)
	defer srv.Close()

	f := Forge{
		Options: Options{URL: srv.URL},
		Log:     silog.Nop(),
	}

	tok := &AuthenticationToken{
		AccessToken: "my-pat",
		AuthType:    AuthTypePAT,
	}
	rid := &RepositoryID{
		url:          srv.URL,
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	_, _ = f.OpenRepository(t.Context(), tok, rid)

	require.NotEmpty(t, gotAuthHeaders,
		"expected at least one request to the server")

	wantEncoded := base64.StdEncoding.EncodeToString(
		[]byte(":my-pat"),
	)
	for _, hdr := range gotAuthHeaders {
		assert.Equal(t, "Basic "+wantEncoded, hdr,
			"server should receive the original PAT")
	}
}

func TestOpenRepository_refreshFailure(t *testing.T) {
	// When 'az account get-access-token' fails,
	// OpenRepository should return an error.

	mockCtrl := gomock.NewController(t)
	execer := xectest.NewMockExecer(mockCtrl)

	execer.EXPECT().
		Run(gomock.Any()).
		Return(&xec.ExitError{
			Stderr: []byte("token expired"),
		})

	old := _execLookPath
	_execLookPath = func(string) (string, error) {
		return "az", nil
	}
	defer func() { _execLookPath = old }()

	f := Forge{
		Options: Options{URL: "https://dev.azure.com"},
		Log:     silog.Nop(),
		Execer:  execer,
	}

	tok := &AuthenticationToken{
		AccessToken: "stale-token",
		AuthType:    AuthTypeAzureCLI,
	}
	rid := &RepositoryID{
		url:          "https://dev.azure.com",
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	_, err := f.OpenRepository(t.Context(), tok, rid)
	require.Error(t, err)
	assert.ErrorContains(t, err, "refresh Azure CLI token")
}

func TestOpenRepository_azNotFound(t *testing.T) {
	// When 'az' is not found on PATH,
	// OpenRepository should return an error
	// for Azure CLI tokens.

	old := _execLookPath
	_execLookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}
	defer func() { _execLookPath = old }()

	f := Forge{
		Options: Options{URL: "https://dev.azure.com"},
		Log:     silog.Nop(),
	}

	tok := &AuthenticationToken{
		AccessToken: "stale-token",
		AuthType:    AuthTypeAzureCLI,
	}
	rid := &RepositoryID{
		url:          "https://dev.azure.com",
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	_, err := f.OpenRepository(t.Context(), tok, rid)
	require.Error(t, err)
	assert.ErrorContains(t, err, "refresh Azure CLI token")
}

func TestOpenRepository_orgScopedURL(t *testing.T) {
	// Regression test:
	// The Azure DevOps SDK requires an organization-scoped URL
	// (e.g. https://dev.azure.com/myorg)
	// for its connection, not just the base URL.
	//
	// Previously, OpenRepository passed only the base URL
	// (e.g. https://dev.azure.com),
	// causing the SDK to request https://dev.azure.com/_apis
	// which returns 404.

	// Track which paths were requested.
	var requestedPaths []string
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPaths = append(requestedPaths, r.URL.Path)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 0,
				"value": []any{},
			})
		}),
	)
	defer srv.Close()

	f := Forge{
		Options: Options{URL: srv.URL},
		Log:     silog.Nop(),
	}

	tok := &AuthenticationToken{AccessToken: "fake-token"}
	rid := &RepositoryID{
		url:          srv.URL,
		organization: "myorg",
		project:      "myproject",
		repository:   "myrepo",
	}

	// OpenRepository will fail because the mock server
	// doesn't return real Azure DevOps responses,
	// but we can verify the request paths
	// include the organization.
	_, _ = f.OpenRepository(t.Context(), tok, rid)

	require.NotEmpty(t, requestedPaths,
		"expected at least one request to the server")
	for _, path := range requestedPaths {
		assert.True(t, len(path) > len("/myorg/"),
			"path %q should start with /myorg/", path)
		assert.Contains(t, path, "/myorg/",
			"request path should include organization")
	}
}
