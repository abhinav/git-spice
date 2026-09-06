package github

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_CheckPullRequestStacks(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/repos/octo/hello/stacks", r.URL.Path)
		return restJSONResponse(http.StatusOK, `{"stacks":[]}`), nil
	}))

	require.NoError(t, gateway.CheckPullRequestStacks(t.Context(), "octo", "hello"))
}

func TestGateway_CreatePullRequestStack(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "api.github.com", r.URL.Host)
		assert.Equal(t, "/repos/octo/hello/stacks", r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		assert.Equal(t, restMediaType, r.Header.Get("Accept"))
		assert.Equal(t, restAPIVersion, r.Header.Get("X-GitHub-Api-Version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"pull_requests":[101,102]}`, string(body))
		return restJSONResponse(http.StatusCreated, `{}`), nil
	}))

	err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
		Owner:        "octo",
		Repo:         "hello",
		PullRequests: []int{101, 102},
	})
	require.NoError(t, err)
}

func TestGateway_CreatePullRequestStackCount(t *testing.T) {
	for _, tt := range []struct {
		name  string
		count int
	}{
		{name: "TooFew", count: 1},
		{name: "TooMany", count: 101},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("HTTP request made with invalid pull request count")
				return nil, nil
			}))

			err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
				Owner:        "octo",
				Repo:         "hello",
				PullRequests: make([]int, tt.count),
			})
			assert.ErrorContains(t, err, "between 2 and 100")
		})
	}
}

func TestGateway_CreatePullRequestStackMaximum(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			PullRequests []int `json:"pull_requests"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Len(t, body.PullRequests, 100)
		return restJSONResponse(http.StatusCreated, `{}`), nil
	}))

	err := gateway.CreatePullRequestStack(t.Context(), &CreatePullRequestStackInput{
		Owner:        "octo",
		Repo:         "hello",
		PullRequests: make([]int, 100),
	})
	require.NoError(t, err)
}

func TestGateway_AddPullRequestsToStack(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/octo/hello/stacks/42/add", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"pull_requests":[103]}`, string(body))
		return restJSONResponse(http.StatusOK, `{}`), nil
	}))

	err := gateway.AddPullRequestsToStack(t.Context(), &AddPullRequestsToStackInput{
		Owner:        "octo",
		Repo:         "hello",
		StackNumber:  42,
		PullRequests: []int{103},
	})
	require.NoError(t, err)
}

func TestGateway_AddPullRequestsToStackCount(t *testing.T) {
	for _, tt := range []struct {
		name  string
		count int
	}{
		{name: "TooFew", count: 0},
		{name: "TooMany", count: 101},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("HTTP request made with invalid pull request count")
				return nil, nil
			}))

			err := gateway.AddPullRequestsToStack(t.Context(), &AddPullRequestsToStackInput{
				Owner:        "octo",
				Repo:         "hello",
				StackNumber:  42,
				PullRequests: make([]int, tt.count),
			})
			assert.ErrorContains(t, err, "between 1 and 100")
		})
	}
}

func TestGateway_UnstackPullRequestStack(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/repos/octo/hello/stacks/42/unstack", r.URL.Path)
		return restJSONResponse(http.StatusOK, `{
			"pull_requests":[{"number":102},{"number":103}]
		}`), nil
	}))

	got, err := gateway.UnstackPullRequestStack(t.Context(), &UnstackPullRequestStackInput{
		Owner:       "octo",
		Repo:        "hello",
		StackNumber: 42,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{102, 103}, got.RemainingPullRequests)
}

func TestGateway_UnstackPullRequestStackComplete(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return restJSONResponse(http.StatusNoContent, ``), nil
	}))

	got, err := gateway.UnstackPullRequestStack(t.Context(), &UnstackPullRequestStackInput{
		Owner:       "octo",
		Repo:        "hello",
		StackNumber: 42,
	})
	require.NoError(t, err)
	assert.Empty(t, got.RemainingPullRequests)
}
