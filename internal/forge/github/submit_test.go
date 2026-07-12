package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func TestRepository_SubmitChange_fromPushRepository(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Query     string `json:"query"`
			Variables struct {
				Owner string         `json:"owner"`
				Repo  string         `json:"repo"`
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		if strings.Contains(body.Query, "repository(owner: $owner, name: $repo)") {
			assert.Equal(t, "test-owner-robot", body.Variables.Owner)
			assert.Equal(t, "test-repo", body.Variables.Repo)

			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"id": "pushRepoID",
					},
				},
			}))
			return
		}

		input := body.Variables.Input
		assert.Equal(t, "repoID", input["repositoryId"])
		assert.Equal(t, "main", input["baseRefName"])
		assert.Equal(t, "test-owner-robot:fork-branch", input["headRefName"])
		assert.Equal(t, "Stabilize nacelles", input["title"])
		assert.Equal(t, "pushRepoID", input["headRepositoryId"])
		created = true

		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"createPullRequest": map[string]any{
					"pullRequest": map[string]any{
						"id":     "prID",
						"number": 55,
						"url":    "https://github.com/test-owner/test-repo/pull/55",
					},
				},
			},
		}))
	}))
	defer srv.Close()
	gatewayClient, err := github.NewGateway(
		srv.URL,
		srv.Client(),
		gatewayTestTokenSource("token"),
	)
	require.NoError(t, err)

	repo, err := newRepository(
		t.Context(), new(Forge),
		"test-owner", "test-repo",
		silogtest.New(t),
		gatewayClient,
		"repoID",
	)
	require.NoError(t, err)

	change, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Stabilize nacelles",
		Base:    "main",
		Head:    "fork-branch",
		PushRepository: &RepositoryID{
			url:   "https://github.com",
			owner: "test-owner-robot",
			name:  "test-repo",
		},
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, &PR{Number: 55, GQLID: "prID"}, change.ID)
	assert.Equal(t,
		"https://github.com/test-owner/test-repo/pull/55",
		change.URL)
}

type gatewayTestTokenSource string

func (s gatewayTestTokenSource) Token(context.Context) (string, error) {
	return string(s), nil
}

func TestRepository_prMetadataCachesUserIDs(t *testing.T) {
	var userQueries int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Query     string `json:"query"`
			Variables struct {
				User0 string         `json:"user0"`
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		switch {
		case strings.Contains(body.Query, "user0:user(login: $user0)"):
			userQueries++
			assert.Equal(t, "alice", body.Variables.User0)

			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"user0": map[string]any{
						"id": "aliceID",
					},
				},
			}))

		case strings.Contains(body.Query, "requestReviews(input:"):
			assert.Equal(t, "prID", body.Variables.Input["pullRequestId"])
			assert.Equal(t, []any{"aliceID"}, body.Variables.Input["userIds"])

			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"requestReviews": map[string]any{
						"clientMutationId": "reviewMutation",
					},
				},
			}))

		case strings.Contains(body.Query, "addAssigneesToAssignable(input:"):
			assert.Equal(t, "prID", body.Variables.Input["assignableId"])
			assert.Equal(t, []any{"aliceID"}, body.Variables.Input["assigneeIds"])

			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"addAssigneesToAssignable": map[string]any{
						"clientMutationId": "assigneeMutation",
					},
				},
			}))

		default:
			t.Fatalf("unexpected query: %s", body.Query)
		}
	}))
	defer srv.Close()
	gatewayClient, err := github.NewGateway(
		srv.URL,
		srv.Client(),
		gatewayTestTokenSource("token"),
	)
	require.NoError(t, err)

	repo, err := newRepository(
		t.Context(), new(Forge),
		"test-owner", "test-repo",
		silogtest.New(t),
		gatewayClient,
		"repoID",
	)
	require.NoError(t, err)

	err = repo.addReviewersToPullRequest(
		t.Context(),
		[]string{"alice"},
		"prID",
	)
	require.NoError(t, err)

	err = repo.addAssigneesToPullRequest(
		t.Context(),
		[]string{"alice"},
		"prID",
	)
	require.NoError(t, err)

	assert.Equal(t, 1, userQueries)
}

func TestRepository_userIDCoalescesConcurrentMisses(t *testing.T) {
	var userQueries atomic.Int32
	firstQueryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseQuery) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		var body struct {
			Query     string `json:"query"`
			Variables struct {
				User0 string `json:"user0"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Contains(t, body.Query, "user0:user(login: $user0)")
		assert.Equal(t, "alice", body.Variables.User0)

		if userQueries.Add(1) == 1 {
			close(firstQueryStarted)
		}

		// Hold the first request open
		// so the second goroutine must join the in-flight lookup
		// instead of reading from a warmed cache.
		<-releaseQuery
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"user0": map[string]any{
					"id": "aliceID",
				},
			},
		}))
	}))
	defer srv.Close()

	repo, err := newRepository(
		t.Context(), new(Forge),
		"test-owner", "test-repo",
		silogtest.New(t),
		newTestGateway(t, srv.URL),
		"repoID",
	)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Go(func() {
		id, err := repo.userID(t.Context(), "alice")
		if err == nil {
			assert.Equal(t, github.ID("aliceID"), id)
		}
		errs <- err
	})

	<-firstQueryStarted

	wg.Go(func() {
		id, err := repo.userID(t.Context(), "alice")
		if err == nil {
			assert.Equal(t, github.ID("aliceID"), id)
		}
		errs <- err
	})

	// The second lookup starts while the first request is still blocked,
	// so a second GraphQL request would mean the miss was not coalesced.
	releaseOnce.Do(func() { close(releaseQuery) })
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), userQueries.Load())
}
