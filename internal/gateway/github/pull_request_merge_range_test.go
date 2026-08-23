package github

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_PullRequestsForMergeRange(t *testing.T) {
	requestNumber := 0
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestNumber++
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

		switch requestNumber {
		case 1:
			assert.Contains(t, request.Query, "pr0:pullRequest(number: $pr0)")
			assert.Contains(t, request.Query, "pr2:pullRequest(number: $pr2)")
			assert.Contains(t, request.Query, "state,isDraft,baseRefName,headRefName,headRefOid")
			assert.Contains(t, request.Query, "headRepository{owner{login},name},stack{id}")
			assert.NotContains(t, request.Query, "labels")
			assert.NotContains(t, request.Query, "entries")
			assert.JSONEq(t, `{
				"owner": "acme",
				"repo": "repo",
				"pr0": 1,
				"pr1": 2,
				"pr2": 3
			}`, string(request.Variables))
			return graphQLResponse(`{
				"data": {"repository": {
					"pr0": {
						"state": "OPEN",
						"isDraft": false,
						"baseRefName": "main",
						"headRefName": "bottom",
						"headRefOid": "hash-1",
						"headRepository": {
							"owner": {"login": "acme"},
							"name": "repo"
						},
						"stack": {"id": "STACK_42"}
					},
					"pr1": {
						"state": "OPEN",
						"isDraft": true,
						"baseRefName": "bottom",
						"headRefName": "top",
						"headRefOid": "hash-2",
						"headRepository": {
							"owner": {"login": "fork"},
							"name": "repo"
						},
						"stack": {"id": "STACK_42"}
					},
					"pr2": null
				}}
			}`), nil

		case 2:
			assert.JSONEq(t, `{"ids":["STACK_42"]}`, string(request.Variables))
			return graphQLResponse(`{
				"data": {"nodes": [{
					"id": "STACK_42",
					"number": 42,
					"entries": {"nodes": [
						{"pullRequest": {"number": 1, "state": "OPEN"}},
						{"pullRequest": {"number": 2, "state": "OPEN"}},
						{"pullRequest": {"number": 4, "state": "MERGED"}}
					]}
				}]}
			}`), nil

		default:
			t.Fatalf("unexpected request %d", requestNumber)
			return nil, nil
		}
	}))

	got, err := gateway.PullRequestsForMergeRange(
		t.Context(),
		"acme",
		"repo",
		[]int{1, 2, 3},
	)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, PullRequestStateOpen, got[0].State)
	assert.False(t, got[0].IsDraft)
	assert.Equal(t, "main", got[0].BaseRefName)
	assert.Equal(t, "bottom", got[0].HeadRefName)
	assert.Equal(t, "hash-1", got[0].HeadRefOID)
	assert.Equal(t, "acme", got[0].HeadRepositoryOwner)
	assert.Equal(t, "repo", got[0].HeadRepositoryName)
	require.NotNil(t, got[0].Stack)
	assert.Equal(t, 42, got[0].Stack.Number)
	assert.Equal(t, []PullRequestStackMember{
		{Number: 1},
		{Number: 2},
	}, got[0].Stack.Members)
	assert.True(t, got[1].IsDraft)
	assert.Equal(t, "fork", got[1].HeadRepositoryOwner)
	assert.Same(t, got[0].Stack, got[1].Stack)
	assert.Nil(t, got[2])
	assert.Equal(t, 2, requestNumber)
}

func TestGateway_PullRequestsForMergeRange_stackStopsResolving(t *testing.T) {
	requestNumber := 0
	gateway := newTestGateway(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requestNumber++
		switch requestNumber {
		case 1:
			return graphQLResponse(`{
				"data": {"repository": {"pr0": {
					"state": "OPEN",
					"isDraft": false,
					"baseRefName": "main",
					"headRefName": "feature",
					"headRefOid": "hash-1",
					"headRepository": {
						"owner": {"login": "acme"},
						"name": "repo"
					},
					"stack": {"id": "STACK_42"}
				}}}
			}`), nil
		case 2:
			return graphQLResponse(`{"data":{"nodes":[null]}}`), nil
		default:
			t.Fatalf("unexpected request %d", requestNumber)
			return nil, nil
		}
	}))

	got, err := gateway.PullRequestsForMergeRange(
		t.Context(),
		"acme",
		"repo",
		[]int{1},
	)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0])
	assert.Nil(t, got[0].Stack)
}

func TestGateway_PullRequestsForMergeRange_empty(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))

	got, err := gateway.PullRequestsForMergeRange(t.Context(), "acme", "repo", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}
