package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

func TestGateway_CreateChange(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
		author     = "jcaptain"
	)

	var gotReq PullRequestCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// CurrentUser: identity comes from the X-AUSERNAME header.
			w.Header().Set("X-AUSERNAME", author)
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			// RepositoryGet: resolve the numeric repository ID.
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			// DefaultReviewers: no project default reviewers configured.
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
				"id":      123,
				"version": 0,
				"title":   gotReq.Title,
				"links": map[string]any{
					"self": []map[string]any{
						{"href": "https://bitbucket.example.com/projects/ENG/repos/warp-core/pull-requests/123/overview"},
					},
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	change, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject:   "Refit the warp core",
		Body:      "Long overdue.",
		Head:      "feature",
		Base:      "main",
		Draft:     true,
		Reviewers: []string{"spock", author}, // author must be filtered out
	})
	require.NoError(t, err)

	// Request body assertions.
	assert.Equal(t, "Refit the warp core", gotReq.Title)
	assert.Equal(t, "Long overdue.", gotReq.Description)
	assert.True(t, gotReq.Draft)

	assert.Equal(t, "refs/heads/feature", gotReq.FromRef.ID)
	assert.Equal(t, slug, gotReq.FromRef.Repository.Slug)
	assert.Equal(t, projectKey, gotReq.FromRef.Repository.Project.Key)

	assert.Equal(t, "refs/heads/main", gotReq.ToRef.ID)
	assert.Equal(t, slug, gotReq.ToRef.Repository.Slug)
	assert.Equal(t, projectKey, gotReq.ToRef.Repository.Project.Key)

	// Author (jcaptain) is filtered out; only spock remains.
	require.Len(t, gotReq.Reviewers, 1)
	assert.Equal(t, "spock", gotReq.Reviewers[0].User.Name)

	// Result assertions.
	assert.Equal(t, int64(123), change.Number)
	assert.Equal(t,
		"https://bitbucket.example.com/projects/ENG/repos/warp-core/pull-requests/123/overview",
		change.URL)
}

func TestGateway_CreateChange_noReviewersSkipsCurrentUser(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	// /application-properties is hit by both CurrentUser and the draft
	// (ApplicationProperties) probe. With Draft unset the draft probe never
	// runs, so the only thing that can hit it here is CurrentUser; tracking
	// the endpoint therefore still proves no current-user lookup happens.
	var appPropsHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			appPropsHit = true
			w.Header().Set("X-AUSERNAME", "jcaptain")
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			// No default reviewers, so the candidate list stays empty.
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost:
			var req PullRequestCreateRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Empty(t, req.Reviewers)
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 7, "version": 0})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	change, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Refit",
		Head:    "feature",
		Base:    "main",
	})
	require.NoError(t, err)

	assert.False(t, appPropsHit,
		"CurrentUser must not be called when there are no reviewers")

	// No self link: URL falls back to the repository-derived change URL.
	assert.Equal(t, int64(7), change.Number)
	assert.Equal(t,
		srv.URL+"/projects/ENG/repos/warp-core/pull-requests/7/overview",
		change.URL)
}

func TestGateway_CreateChange_mergesDefaultReviewers(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
		author     = "jcaptain"
	)

	tests := []struct {
		name             string
		defaultReviewers []map[string]any
		reviewers        []string
		want             []string
	}{
		{
			// Default reviewers come first, the author is filtered out, and
			// the explicit reviewer follows.
			name:             "DefaultsFirst",
			defaultReviewers: []map[string]any{{"name": "alice", "id": 10}},
			reviewers:        []string{"spock", author},
			want:             []string{"alice", "spock"},
		},
		{
			// A default reviewer that is also requested explicitly appears
			// only once, keeping its defaults-first position.
			name:             "DedupAcrossSources",
			defaultReviewers: []map[string]any{{"name": "alice", "id": 10}},
			reviewers:        []string{"alice", "spock"},
			want:             []string{"alice", "spock"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotReq PullRequestCreateRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rest/api/1.0/application-properties":
					w.Header().Set("X-AUSERNAME", author)
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})

				case r.Method == http.MethodGet &&
					r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

				case r.Method == http.MethodGet &&
					r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
					gatewayWriteJSON(t, w, http.StatusOK, tt.defaultReviewers)

				case r.Method == http.MethodPost &&
					r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
					require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
					gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 1, "version": 0})

				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
				}
			}))
			defer srv.Close()

			gw := newOpsTestServerGateway(t, srv)
			_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
				Subject:   "Refit",
				Head:      "feature",
				Base:      "main",
				Reviewers: tt.reviewers,
			})
			require.NoError(t, err)

			got := make([]string, len(gotReq.Reviewers))
			for i, rev := range gotReq.Reviewers {
				got[i] = rev.User.Name
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGateway_CreateChange_defaultReviewersFailureIsBestEffort(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
		author     = "jcaptain"
	)

	tests := []struct {
		name string
		// repoGetStatus and reviewersStatus select where the best-effort
		// failure occurs.
		repoGetStatus   int
		reviewersStatus int
	}{
		{
			// The repository-ID lookup fails, so default reviewers are
			// skipped entirely.
			name:            "RepositoryGetFails",
			repoGetStatus:   http.StatusInternalServerError,
			reviewersStatus: http.StatusOK,
		},
		{
			// The repository ID resolves but the default-reviewers call
			// fails.
			name:            "DefaultReviewersFails",
			repoGetStatus:   http.StatusOK,
			reviewersStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotReq PullRequestCreateRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rest/api/1.0/application-properties":
					w.Header().Set("X-AUSERNAME", author)
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "9.4.0"})

				case r.Method == http.MethodGet &&
					r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
					if tt.repoGetStatus != http.StatusOK {
						http.Error(w, "boom", tt.repoGetStatus)
						return
					}
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

				case r.Method == http.MethodGet &&
					r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
					if tt.reviewersStatus != http.StatusOK {
						http.Error(w, "boom", tt.reviewersStatus)
						return
					}
					gatewayWriteJSON(t, w, http.StatusOK, []any{})

				case r.Method == http.MethodPost &&
					r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
					require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
					gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 1, "version": 0})

				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
				}
			}))
			defer srv.Close()

			// The create still succeeds, using only the explicit reviewer.
			gw := newOpsTestServerGateway(t, srv)
			_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
				Subject:   "Refit",
				Head:      "feature",
				Base:      "main",
				Reviewers: []string{"spock"},
			})
			require.NoError(t, err)

			require.Len(t, gotReq.Reviewers, 1)
			assert.Equal(t, "spock", gotReq.Reviewers[0].User.Name)
		})
	}
}

// When only default reviewers are present and the current-user lookup fails,
// the defaults are dropped (since self cannot be filtered out) and the create
// proceeds, consistent with default reviewers being best-effort everywhere.
func TestGateway_CreateChange_defaultReviewersOnlyCurrentUserErrorIsBestEffort(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	var gotReq PullRequestCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// CurrentUser cannot be resolved. (Draft is unset, so the draft
			// probe never hits this endpoint.)
			http.Error(w, "boom", http.StatusInternalServerError)

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			gatewayWriteJSON(t, w, http.StatusOK, []map[string]any{{"name": "alice"}})

		case r.Method == http.MethodPost &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 1, "version": 0})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	// No explicit reviewers: the failed current-user lookup must not fail
	// the create.
	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Refit",
		Head:    "feature",
		Base:    "main",
	})
	require.NoError(t, err)

	// The default reviewer is dropped because self could not be resolved.
	assert.Empty(t, gotReq.Reviewers)
}

// When explicit reviewers are requested, the current-user lookup must succeed
// so they can be self-filtered before they are sent; a failure is fatal.
func TestGateway_CreateChange_explicitReviewersCurrentUserErrorFails(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// CurrentUser cannot be resolved. (Draft is unset, so the draft
			// probe never hits this endpoint.)
			http.Error(w, "boom", http.StatusInternalServerError)

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost:
			t.Errorf("pull request must not be created when current-user lookup fails")
			http.Error(w, "unexpected create", http.StatusInternalServerError)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject:   "Refit",
		Head:      "feature",
		Base:      "main",
		Reviewers: []string{"spock"},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "identify current user")
}

func TestGateway_CreateChange_draftDowngradedOnOldServer(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	var gotReq PullRequestCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// Server too old for draft pull requests.
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "8.17.0"})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{"id": 1, "version": 0})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	var logBuffer bytes.Buffer
	gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: projectKey,
		slug:       slug,
	}, silog.New(&logBuffer, nil))

	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Refit",
		Head:    "feature",
		Base:    "main",
		Draft:   true,
	})
	require.NoError(t, err)

	// The old server cannot honor draft, so the create request is
	// downgraded to a regular pull request, with a warning.
	assert.False(t, gotReq.Draft)
	assert.Contains(t, logBuffer.String(),
		"Bitbucket Data Center < 8.18 does not support draft pull requests; "+
			"creating a regular pull request")
}

func TestGateway_CreateChange_draftKeptOnNewServer(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	var gotReq PullRequestCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// Exactly the first version that supports draft pull requests.
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"version": "8.18.0"})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
				"id": 1, "version": 0, "draft": true,
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	change, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Refit",
		Head:    "feature",
		Base:    "main",
		Draft:   true,
	})
	require.NoError(t, err)

	// A new-enough server honors draft, so the flag is sent as-is.
	assert.True(t, gotReq.Draft)
	assert.True(t, change.Draft)
}

func TestGateway_CreateChange_draftBestEffortUnknownVersion(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	var gotReq PullRequestCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/1.0/application-properties":
			// Version cannot be read; the gateway proceeds best-effort.
			http.Error(w, "boom", http.StatusInternalServerError)

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug+"/pull-requests":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
			// The server echoes a non-draft pull request, exercising the
			// post-create warning path.
			gatewayWriteJSON(t, w, http.StatusCreated, map[string]any{
				"id": 1, "version": 0, "draft": false,
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	var logBuffer bytes.Buffer
	gw := newTestServerGateway(t, srv.URL, &serverRepositoryID{
		url:        srv.URL,
		projectKey: projectKey,
		slug:       slug,
	}, silog.New(&logBuffer, nil))

	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Refit",
		Head:    "feature",
		Base:    "main",
		Draft:   true,
	})
	require.NoError(t, err)

	// With an unreadable version, draft:true is still sent best-effort,
	// and the non-draft result is called out.
	assert.True(t, gotReq.Draft)
	assert.Contains(t, logBuffer.String(),
		"Server created a non-draft pull request; "+
			"the draft flag may be unsupported on this Bitbucket Data Center version")
}

func TestGateway_CreateChange_destinationBranchMissing(t *testing.T) {
	const (
		projectKey = "ENG"
		slug       = "warp-core"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/api/1.0/projects/"+projectKey+"/repos/"+slug:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 42, "slug": slug})

		case r.Method == http.MethodGet &&
			r.URL.Path == "/rest/default-reviewers/1.0/projects/"+projectKey+"/repos/"+slug+"/reviewers":
			gatewayWriteJSON(t, w, http.StatusOK, []any{})

		case r.Method == http.MethodPost:
			gatewayWriteJSON(t, w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]any{
					{
						"message":       `The branch "refs/heads/main" does not exist.`,
						"exceptionName": "com.atlassian.bitbucket.validation.ArgumentValidationException",
					},
				},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject: "Refit",
		Head:    "feature",
		Base:    "main",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, `The branch "refs/heads/main" does not exist.`)
}

// crossRepositoryID is a [forge.RepositoryID] stand-in
// for a repository other than the gateway's.
type crossRepositoryID string

// String returns the repository name verbatim.
func (c crossRepositoryID) String() string { return string(c) }

// ChangeURL implements [forge.RepositoryID]; it is never called.
func (crossRepositoryID) ChangeURL(forge.ChangeID) string { return "" }

func TestGateway_CreateChange_crossRepositoryUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.CreateChange(t.Context(), bitbucket.CreateChangeRequest{
		Subject:        "Refit",
		Head:           "feature",
		Base:           "main",
		PushRepository: crossRepositoryID("ENG/warp-core-fork"),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "cross-repository")
}

func TestGateway_GetChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, prItemPath(42), r.URL.Path)
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"id":      42,
			"version": 1,
			"title":   "Test PR",
			"state":   "MERGED",
			"fromRef": map[string]any{"latestCommit": "deadbeef"},
			"toRef":   map[string]any{"displayId": "main"},
		})
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	change, err := gw.GetChange(t.Context(), 42)
	require.NoError(t, err)

	assert.Equal(t, int64(42), change.Number)
	assert.Equal(t, "Test PR", change.Subject)
	assert.Equal(t, "main", change.BaseName)
	assert.Equal(t, git.Hash("deadbeef"), change.HeadHash)
	assert.Equal(t, forge.ChangeMerged, change.State)
	// No self link: URL falls back to the repository-derived change URL.
	assert.Equal(t, srv.URL+"/projects/ENG/repos/warp-core/pull-requests/42/overview", change.URL)
}

func TestGateway_GetChange_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.GetChange(t.Context(), 99)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMergeabilityFromAPI(t *testing.T) {
	tests := []struct {
		name   string
		status *MergeStatus
		want   forge.ChangeMergeability
	}{
		{
			name: "Nil",
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityUnknown,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "CanMerge",
			status: &MergeStatus{
				CanMerge: true,
				Outcome:  "CLEAN",
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityReady,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
		{
			name: "Conflicted",
			status: &MergeStatus{
				Conflicted: true,
				Outcome:    "UNKNOWN",
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			},
		},
		{
			name: "ConflictedOutcome",
			status: &MergeStatus{
				Outcome: "CONFLICTED",
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			},
		},
		{
			name: "Vetoed",
			status: &MergeStatus{
				CanMerge: false,
				Outcome:  "CLEAN",
				Vetoes: []Veto{{
					SummaryMessage: "Needs approval",
				}},
			},
			want: forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonUnknown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeabilityFromAPI(tt.status))
		})
	}
}

func TestGateway_ChangeMergeability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, prItemPath(42)+"/merge", r.URL.Path)
		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"canMerge":   false,
			"conflicted": true,
			"outcome":    "CONFLICTED",
		})
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	got, err := gw.ChangeMergeability(t.Context(), 42)
	require.NoError(t, err)
	assert.Equal(t, forge.ChangeMergeability{
		State:  forge.ChangeMergeabilityBlocked,
		Reason: forge.ChangeMergeabilityReasonConflicts,
	}, got)
}

func TestGateway_ChangeMergeability_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	_, err := gw.ChangeMergeability(t.Context(), 99)
	require.Error(t, err)
	assert.ErrorContains(t, err, "get mergeability")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGateway_FindChangesByBranch(t *testing.T) {
	var gotQuery struct {
		at, direction, state string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, prListPath(), r.URL.Path)
		gotQuery.at = r.URL.Query().Get("at")
		gotQuery.direction = r.URL.Query().Get("direction")
		gotQuery.state = r.URL.Query().Get("state")

		gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
			"isLastPage": true,
			"values": []map[string]any{
				{
					"id":      11,
					"version": 3,
					"title":   "Refit the warp core",
					"state":   "OPEN",
					"open":    true,
					"draft":   true,
					"fromRef": map[string]any{
						"displayId":    "feature",
						"latestCommit": "abc123",
					},
					"toRef": map[string]any{
						"displayId": "develop",
					},
					"reviewers": []map[string]any{
						{"user": map[string]any{"name": "spock"}},
						{"user": map[string]any{"name": "uhura"}},
					},
					"links": map[string]any{
						"self": []map[string]any{
							{"href": "https://bb.example.com/pr/11"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	changes, err := gw.FindChangesByBranch(t.Context(), "feature", bitbucket.FindChangesOptions{})
	require.NoError(t, err)

	// A zero state means "all states" (per the gateway contract),
	// which Bitbucket Data Center expresses as the ALL filter.
	assert.Equal(t, "refs/heads/feature", gotQuery.at)
	assert.Equal(t, "OUTGOING", gotQuery.direction)
	assert.Equal(t, "ALL", gotQuery.state)

	require.Len(t, changes, 1)
	got := changes[0]
	assert.Equal(t, int64(11), got.Number)
	assert.Equal(t, "https://bb.example.com/pr/11", got.URL)
	assert.Equal(t, forge.ChangeOpen, got.State)
	assert.Equal(t, "Refit the warp core", got.Subject)
	assert.Equal(t, "develop", got.BaseName)
	assert.Equal(t, git.Hash("abc123"), got.HeadHash)
	assert.True(t, got.Draft)
	assert.Equal(t, []string{"spock", "uhura"}, got.Reviewers)
}

func TestGateway_FindChangesByBranch_crossRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	changes, err := gw.FindChangesByBranch(
		t.Context(),
		"feature",
		bitbucket.FindChangesOptions{
			PushRepository: crossRepositoryID("OTHER/warp-core"),
		},
	)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestGateway_FindChangesByBranch_stateFilter(t *testing.T) {
	tests := []struct {
		name      string
		state     forge.ChangeState
		wantState string
	}{
		{"AllStates", 0, "ALL"},
		{"Open", forge.ChangeOpen, "OPEN"},
		{"Merged", forge.ChangeMerged, "MERGED"},
		{"Closed", forge.ChangeClosed, "DECLINED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotState string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotState = r.URL.Query().Get("state")
				gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
					"isLastPage": true,
					"values":     []map[string]any{},
				})
			}))
			defer srv.Close()

			gw := newOpsTestServerGateway(t, srv)
			_, err := gw.FindChangesByBranch(t.Context(), "feature",
				bitbucket.FindChangesOptions{State: tt.state})
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, gotState)
		})
	}
}

func TestGateway_FindChangesByBranch_respectsLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"Limited", 2, 2},
		// A zero limit means no limit; the gateway returns everything
		// and leaves defaulting to the caller.
		{"NoLimit", 0, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				values := make([]map[string]any, 0, 5)
				for i := int64(1); i <= 5; i++ {
					values = append(values, map[string]any{
						"id": i, "title": "PR", "state": "OPEN",
					})
				}
				gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
					"isLastPage": true,
					"values":     values,
				})
			}))
			defer srv.Close()

			gw := newOpsTestServerGateway(t, srv)
			changes, err := gw.FindChangesByBranch(t.Context(), "feature",
				bitbucket.FindChangesOptions{Limit: tt.limit})
			require.NoError(t, err)
			assert.Len(t, changes, tt.want)
		})
	}
}

func TestGateway_UpdateChange_baseAndReviewers(t *testing.T) {
	var gotUpdate PullRequestUpdateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == prItemPath(7):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"id":          7,
				"version":     4,
				"title":       "Original title",
				"description": "Original description",
				"state":       "OPEN",
				"author":      map[string]any{"user": map[string]any{"name": "jcaptain"}},
				"reviewers": []map[string]any{
					// A default reviewer auto-injected by Bitbucket DC.
					{"user": map[string]any{"name": "default-rev"}},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == prItemPath(7):
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotUpdate))
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 7, "version": 5, "title": "Original title"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.UpdateChange(t.Context(), 7, bitbucket.ChangeUpdate{
		Base:         "develop",
		AddReviewers: []string{"spock", "jcaptain"}, // author must be excluded
	})
	require.NoError(t, err)

	// Version, current title, and current description are carried so the
	// wholesale update does not clear them.
	assert.Equal(t, 4, gotUpdate.Version)
	assert.Equal(t, "Original title", gotUpdate.Title)
	require.NotNil(t, gotUpdate.Description)
	assert.Equal(t, "Original description", *gotUpdate.Description)

	// Base change sets the new toRef.
	require.NotNil(t, gotUpdate.ToRef)
	assert.Equal(t, "refs/heads/develop", gotUpdate.ToRef.ID)

	// Reviewers: existing default reviewer preserved, spock added,
	// author (jcaptain) excluded.
	names := make([]string, len(gotUpdate.Reviewers))
	for i, rev := range gotUpdate.Reviewers {
		names[i] = rev.User.Name
	}
	assert.Equal(t, []string{"default-rev", "spock"}, names)
}

func TestGateway_UpdateChange_conflictRetries(t *testing.T) {
	var (
		getCount int
		putCount int
		versions []int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount++
			// First GET reports version 4 (stale); refetch reports 9.
			version := 4
			if getCount > 1 {
				version = 9
			}
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"id": 7, "version": version, "title": "T",
				"author": map[string]any{"user": map[string]any{"name": "jcaptain"}},
			})
		case http.MethodPut:
			putCount++
			var req PullRequestUpdateRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			versions = append(versions, req.Version)
			if putCount == 1 {
				// Reject the first PUT with a version conflict.
				gatewayWriteJSON(t, w, http.StatusConflict, map[string]any{
					"errors": []map[string]any{{"message": "out of date"}},
				})
				return
			}
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 7, "version": 10, "title": "T"})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.UpdateChange(t.Context(), 7, bitbucket.ChangeUpdate{
		AddReviewers: []string{"spock"},
	})
	require.NoError(t, err)

	assert.Equal(t, 2, getCount, "expected an initial GET plus one refetch")
	assert.Equal(t, 2, putCount, "expected the PUT to be retried once")
	// The retry carries the refreshed version.
	assert.Equal(t, []int{4, 9}, versions)
}

func TestGateway_buildUpdateRequest(t *testing.T) {
	reviewer := func(name string) Reviewer {
		return Reviewer{User: User{Name: name}}
	}
	mkPR := func(author string, existing ...string) *PullRequest {
		pr := &PullRequest{
			Version:     7,
			Title:       "Refit",
			Description: "the warp core needs a refit",
		}
		pr.Author.User.Name = author
		for _, name := range existing {
			pr.Reviewers = append(pr.Reviewers, reviewer(name))
		}
		return pr
	}

	tests := []struct {
		name          string
		pr            *PullRequest
		update        bitbucket.ChangeUpdate
		wantReviewers []string
		wantToRef     string
	}{
		{
			// The Data Center update replaces fields wholesale, so a
			// base-only retarget must carry the existing reviewers.
			name:          "BaseOnlyPreservesReviewers",
			pr:            mkPR("kirk", "uhura", "spock"),
			update:        bitbucket.ChangeUpdate{Base: "main"},
			wantReviewers: []string{"uhura", "spock"},
			wantToRef:     "refs/heads/main",
		},
		{
			name:          "ExistingPreservedAndAdded",
			pr:            mkPR("kirk", "uhura"),
			update:        bitbucket.ChangeUpdate{AddReviewers: []string{"spock"}},
			wantReviewers: []string{"uhura", "spock"},
		},
		{
			name: "DedupExistingAndAdded",
			pr:   mkPR("kirk", "spock"),
			update: bitbucket.ChangeUpdate{
				AddReviewers: []string{"spock", "uhura", "uhura"},
			},
			wantReviewers: []string{"spock", "uhura"},
		},
		{
			name: "ExcludesAuthor",
			pr:   mkPR("kirk", "uhura"),
			update: bitbucket.ChangeUpdate{
				AddReviewers: []string{"kirk", "spock"},
			},
			wantReviewers: []string{"uhura", "spock"},
		},
		{
			name: "SkipsEmptyNames",
			pr:   mkPR("kirk", ""),
			update: bitbucket.ChangeUpdate{
				AddReviewers: []string{"", "spock"},
			},
			wantReviewers: []string{"spock"},
		},
	}

	gw := newTestServerGateway(t,
		"https://bitbucket.example.com",
		&serverRepositoryID{
			url:        "https://bitbucket.example.com",
			projectKey: testProjectKey,
			slug:       testSlug,
		},
		silog.Nop(),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := gw.buildUpdateRequest(tt.pr, tt.update)

			// Version, title, and description are always carried over.
			assert.Equal(t, tt.pr.Version, req.Version)
			assert.Equal(t, tt.pr.Title, req.Title)
			require.NotNil(t, req.Description)
			assert.Equal(t, tt.pr.Description, *req.Description)

			names := make([]string, len(req.Reviewers))
			for i, rev := range req.Reviewers {
				names[i] = rev.User.Name
			}
			assert.Equal(t, tt.wantReviewers, names)

			if tt.wantToRef == "" {
				assert.Nil(t, req.ToRef)
			} else {
				require.NotNil(t, req.ToRef)
				assert.Equal(t, tt.wantToRef, req.ToRef.ID)
			}
		})
	}
}

func TestGateway_MergeChange_strategyMapping(t *testing.T) {
	tests := []struct {
		name         string
		method       forge.MergeMethod
		wantStrategy string
		wantOmitted  bool
	}{
		{"Default", forge.MergeMethodDefault, "", true},
		{"Merge", forge.MergeMethodMerge, "no-ff", false},
		{"Squash", forge.MergeMethodSquash, "squash", false},
		{"Rebase", forge.MergeMethodRebase, "rebase-no-ff", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				gotVersion string
				gotRaw     map[string]any
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge"):
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"canMerge": true, "outcome": "CLEAN"})
				case r.Method == http.MethodGet:
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 2, "state": "OPEN"})
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge"):
					gotVersion = r.URL.Query().Get("version")
					require.NoError(t, json.NewDecoder(r.Body).Decode(&gotRaw))
					gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 3, "state": "MERGED"})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()

			gw := newOpsTestServerGateway(t, srv)
			require.NoError(t, gw.MergeChange(t.Context(), 5, tt.method))

			assert.Equal(t, "2", gotVersion)
			if tt.wantOmitted {
				assert.NotContains(t, gotRaw, "strategyId")
			} else {
				assert.Equal(t, tt.wantStrategy, gotRaw["strategyId"])
			}
		})
	}
}

func TestGateway_MergeChange_conflictRetries(t *testing.T) {
	var (
		getCount   int
		mergeCount int
		versions   []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge"):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"canMerge": true, "outcome": "CLEAN"})
		case r.Method == http.MethodGet:
			getCount++
			version := 2
			if getCount > 1 {
				version = 8
			}
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": version, "state": "OPEN"})
		case strings.HasSuffix(r.URL.Path, "/merge"):
			mergeCount++
			versions = append(versions, r.URL.Query().Get("version"))
			if mergeCount == 1 {
				gatewayWriteJSON(t, w, http.StatusConflict, map[string]any{
					"errors": []map[string]any{{"message": "stale"}},
				})
				return
			}
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 9, "state": "MERGED"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	require.NoError(t, gw.MergeChange(t.Context(), 5, forge.MergeMethodDefault))

	assert.Equal(t, 2, getCount)
	assert.Equal(t, 2, mergeCount)
	assert.Equal(t, []string{"2", "8"}, versions)
}

func TestGateway_MergeChange_disabledStrategyFails(t *testing.T) {
	// A merge strategy the repository does not allow is surfaced as an
	// error rather than silently merging with a different strategy.
	var merges int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge"):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"canMerge": true, "outcome": "CLEAN"})
		case r.Method == http.MethodGet:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 2, "state": "OPEN"})
		case strings.HasSuffix(r.URL.Path, "/merge"):
			merges++
			gatewayWriteJSON(t, w, http.StatusBadRequest, map[string]any{
				"errors": []map[string]any{
					{"message": "The merge strategy squash is not enabled for this repository."},
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.MergeChange(t.Context(), 5, forge.MergeMethodSquash)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not enabled for this repository")
	assert.Equal(t, 1, merges, "the merge must not be retried")
}

func TestGateway_MergeChange_blockedByVeto(t *testing.T) {
	var merged bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge"):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{
				"canMerge":   false,
				"conflicted": false,
				"outcome":    "UNKNOWN",
				"vetoes": []map[string]any{
					{"summaryMessage": "requires 2 approvals"},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge"):
			merged = true
			t.Errorf("merge must not be attempted when blocked by a veto")
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	err := gw.MergeChange(t.Context(), 5, forge.MergeMethodDefault)
	require.Error(t, err)
	assert.ErrorIs(t, err, bitbucket.ErrMergeBlocked)
	assert.Contains(t, err.Error(), "requires 2 approvals")
	assert.False(t, merged, "the merge POST must never be issued")
}

func TestGateway_MergeChange_probeErrorFallsThrough(t *testing.T) {
	var (
		versionFetched bool
		merged         bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge"):
			// The pre-merge probe fails; merge must proceed regardless.
			http.Error(w, "boom", http.StatusInternalServerError)
		case r.Method == http.MethodGet:
			versionFetched = true
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 2, "state": "OPEN"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge"):
			merged = true
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 3, "state": "MERGED"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	require.NoError(t, gw.MergeChange(t.Context(), 5, forge.MergeMethodDefault))
	assert.True(t, versionFetched, "the version GET must still happen")
	assert.True(t, merged, "the merge POST must still happen")
}

func TestGateway_MergeChange_canMergeCleanProceeds(t *testing.T) {
	var merged bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge"):
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"canMerge": true, "outcome": "CLEAN"})
		case r.Method == http.MethodGet:
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 2, "state": "OPEN"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge"):
			merged = true
			gatewayWriteJSON(t, w, http.StatusOK, map[string]any{"id": 5, "version": 3, "state": "MERGED"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	gw := newOpsTestServerGateway(t, srv)
	require.NoError(t, gw.MergeChange(t.Context(), 5, forge.MergeMethodDefault))
	assert.True(t, merged, "a clean pre-merge check must let the merge proceed")
}
