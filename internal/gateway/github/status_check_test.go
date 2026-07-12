package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_StatusChecks_rejectsUnknownEnumValue(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {
			"node": {
				"commits": {
					"nodes": [{
						"commit": {
							"statusCheckRollup": {
								"contexts": {
				"nodes": [
					{
						"context": "",
						"state": "RECALIBRATING",
						"createdAt": "2026-07-11T00:00:00Z"
					},
					{
						"name": "",
						"status": "IN_PROGRESS",
						"conclusion": null,
						"startedAt": "2026-07-11T00:00:00Z",
						"completedAt": null,
						"checkSuite": {"workflowRun": {
							"event": "push",
							"workflow": {"name": "CI"}
						}}
					}
				],
				"pageInfo": {"endCursor": "cursor", "hasNextPage": false}
								}
							}
						}
					}]
				}
			}
		}
	}`)

	var gotErr error
	for _, err := range gateway.StatusChecks(t.Context(), "PR_1", nil) {
		gotErr = err
	}
	assert.ErrorContains(t, gotErr, `unknown github.StatusState: "RECALIBRATING"`)
}

func TestGateway_StatusChecks_normalizesKnownMembers(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {
			"node": {
				"commits": {
					"nodes": [{
						"commit": {
							"statusCheckRollup": {
								"contexts": {
				"nodes": [
					{
						"context": "build",
						"state": "SUCCESS",
						"createdAt": "2026-07-11T00:00:00Z"
					},
					{
						"name": "test",
						"status": "COMPLETED",
						"conclusion": "SUCCESS",
						"startedAt": "2026-07-11T00:00:00Z",
						"completedAt": "2026-07-11T00:01:00Z",
						"checkSuite": {"workflowRun": {
							"event": "push",
							"workflow": {"name": "CI"}
						}}
					}
				],
				"pageInfo": {"endCursor": "done", "hasNextPage": false}
								}
							}
						}
					}]
				}
			}
		}
	}`)

	var checks []StatusCheck
	for check, err := range gateway.StatusChecks(t.Context(), "PR_1", nil) {
		require.NoError(t, err)
		checks = append(checks, check)
	}
	require.Len(t, checks, 2)

	status, ok := checks[0].(*StatusContext)
	require.True(t, ok)
	assert.Equal(t, "build", status.Context)
	assert.Equal(t, StatusStateSuccess, status.State)

	checkRun, ok := checks[1].(*CheckRun)
	require.True(t, ok)
	assert.Equal(t, "test", checkRun.Name)
	assert.Equal(t, CheckConclusionStateSuccess, *checkRun.Conclusion)
	assert.Equal(t, "CI", checkRun.CheckSuite.WorkflowRun.Workflow.Name)
}

func TestGateway_StatusChecks_rejectsAmbiguousUnionMember(t *testing.T) {
	gateway := newResponseGateway(t, `{"data":{"node":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[{"context":"status","name":"check"}],"pageInfo":{}}}}}]}}}}`)

	var gotErr error
	for _, err := range gateway.StatusChecks(t.Context(), "PR_1", nil) {
		gotErr = err
	}
	assert.ErrorContains(t, gotErr, "ambiguous union member")
}

func TestGateway_StatusChecks_ignoresUnknownUnionMember(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string `json:"query"`
			Variables struct {
				After *string `json:"after"`
				ID    ID      `json:"id"`
			} `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "query($after:String$id:ID!){node(id: $id){... on PullRequest{commits(last: 1){nodes{commit{statusCheckRollup{contexts(first: 100, after: $after){nodes{... on StatusContext{context,state,createdAt},... on CheckRun{name,checkSuite{workflowRun{event,workflow{name}}},status,conclusion,startedAt,completedAt}},pageInfo{endCursor,hasNextPage}}}}}}}}}", request.Query)
		assert.Nil(t, request.Variables.After)
		assert.Equal(t, ID("PR_1"), request.Variables.ID)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":{"node":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[{}],"pageInfo":{"endCursor":"cursor","hasNextPage":false}}}}}]}}}}`)),
		}, nil
	}))

	for _, err := range gateway.StatusChecks(t.Context(), "PR_1", nil) {
		require.NoError(t, err)
	}
}

func TestGateway_StatusChecks_paginates(t *testing.T) {
	requestNum := 0
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, "query($after:String$id:ID!){node(id: $id){... on PullRequest{commits(last: 1){nodes{commit{statusCheckRollup{contexts(first: 2, after: $after){nodes{... on StatusContext{context,state,createdAt},... on CheckRun{name,checkSuite{workflowRun{event,workflow{name}}},status,conclusion,startedAt,completedAt}},pageInfo{endCursor,hasNextPage}}}}}}}}}", request.Query)

		requestNum++
		var response string
		switch requestNum {
		case 1:
			assert.JSONEq(t, `{
				"after": null,
				"id": "PR_1"
			}`, string(request.Variables))
			response = `{
				"data": {"node": {"commits": {"nodes": [{"commit": {
					"statusCheckRollup": {"contexts": {
						"nodes": [{
							"context": "build",
							"state": "SUCCESS",
							"createdAt": "2026-07-11T00:00:00Z"
						}],
						"pageInfo": {"endCursor": "next", "hasNextPage": true}
					}}
				}}]}}}
			}`
		case 2:
			assert.JSONEq(t, `{
				"after": "next",
				"id": "PR_1"
			}`, string(request.Variables))
			response = `{
				"data": {"node": {"commits": {"nodes": [{"commit": {
					"statusCheckRollup": {"contexts": {
						"nodes": [{
							"context": "test",
							"state": "SUCCESS",
							"createdAt": "2026-07-11T00:01:00Z"
						}],
						"pageInfo": {"endCursor": "", "hasNextPage": false}
					}}
				}}]}}}
			}`
		default:
			t.Fatalf("unexpected request %d", requestNum)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(response)),
		}, nil
	}))

	var contexts []string
	for check, err := range gateway.StatusChecks(t.Context(), "PR_1", &PaginationOptions{ItemsPerPage: 2}) {
		require.NoError(t, err)
		contexts = append(contexts, check.(*StatusContext).Context)
	}
	assert.Equal(t, []string{"build", "test"}, contexts)
	assert.Equal(t, 2, requestNum)
}
