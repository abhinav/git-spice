package github

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_FindPullRequestsByBranches(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string         `json:"query"`
			Variables jsontext.Value `json:"variables"`
		}
		require.NoError(t, json.UnmarshalRead(r.Body, &request))
		assert.Contains(t, request.Query, "branch0:pullRequests")
		assert.Contains(t, request.Query, "branch1:pullRequests")
		assert.JSONEq(t, `{
			"branch0": "one",
			"branch1": "two",
			"limit": 10,
			"owner": "acme",
			"repo": "repo",
			"states": ["OPEN", "CLOSED", "MERGED"]
		}`, string(request.Variables))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`{
				"data": {"repository": {
					"branch0": {"nodes": [{
						"id": "PR_1",
						"number": 1,
						"state": "OPEN",
						"headRefOid": "abc",
						"headRepository": {
							"owner": {"login": "acme"},
							"name": "repo"
						}
					}]},
					"branch1": {"nodes": []}
				}}
			}`)),
		}, nil
	}))

	got, err := gateway.FindPullRequestsByBranches(t.Context(), &FindPullRequestsByBranchesRequest{
		Owner:    "acme",
		Repo:     "repo",
		Branches: []string{"one", "two"},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, got[0], 1)
	assert.Equal(t, ID("PR_1"), got[0][0].ID)
	assert.Equal(t, 1, got[0][0].Number)
	assert.Equal(t, PullRequestStateOpen, got[0][0].State)
	assert.Equal(t, "abc", got[0][0].HeadRefOID)
	assert.Equal(t, "acme", got[0][0].HeadRepository.Owner.Login)
	assert.Equal(t, "repo", got[0][0].HeadRepository.Name)
	assert.Empty(t, got[1])
}

func TestGateway_FindPullRequestsByBranches_empty(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))

	got, err := gateway.FindPullRequestsByBranches(t.Context(), &FindPullRequestsByBranchesRequest{
		Owner: "acme",
		Repo:  "repo",
	})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGateway_FindPullRequests(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"pullRequests": {"nodes": [{
			"id": "PR_1", "number": 1, "state": "OPEN"
		}]}}}
	}`)
	got, err := gateway.FindPullRequests(t.Context(), "acme", "repo", "topic", 10, []PullRequestState{PullRequestStateOpen})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, ID("PR_1"), got[0].ID)
}

func TestGateway_PullRequest(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"repository": {"pullRequest": {
			"id": "PR_1", "number": 1, "state": "OPEN"
		}}}
	}`)
	got, err := gateway.PullRequest(t.Context(), "acme", "repo", 1)
	require.NoError(t, err)
	assert.Equal(t, ID("PR_1"), got.ID)
}
