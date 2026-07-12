package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateway_AddPullRequestMetadata(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, compactGraphQL(`
			mutation(
				$labels:AddLabelsToLabelableInput!
				$reviews:RequestReviewsInput!
				$assignees:AddAssigneesToAssignableInput!
			){
				labels:addLabelsToLabelable(input: $labels){clientMutationId},
				reviews:requestReviews(input: $reviews){clientMutationId},
				assignees:addAssigneesToAssignable(input: $assignees){clientMutationId}
			}
		`), request.Query)
		assert.JSONEq(t, `{
			"assignees": {
				"assignableId": "PR_1",
				"assigneeIds": ["U_2"]
			},
			"labels": {
				"labelableId": "PR_1",
				"labelIds": ["L_1"]
			},
			"reviews": {
				"pullRequestId": "PR_1",
				"userIds": ["U_1"],
				"teamIds": ["T_1"],
				"union": true
			}
		}`, string(request.Variables))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`{
				"data": {
					"labels": {},
					"reviews": null,
					"assignees": {}
				},
				"errors": [{
					"message": "missing reviewer",
					"path": ["reviews"],
					"type": "NOT_FOUND"
				}]
			}`)),
		}, nil
	}))

	err := gateway.AddPullRequestMetadata(t.Context(), &PullRequestMetadataInput{
		PullRequestID:   "PR_1",
		LabelIDs:        []ID{"L_1"},
		ReviewerUserIDs: []ID{"U_1"},
		ReviewerTeamIDs: []ID{"T_1"},
		AssigneeIDs:     []ID{"U_2"},
	})
	require.Error(t, err)

	var graphQLError *Error
	require.True(t, errors.As(err, &graphQLError))
	assert.Equal(t, []any{"reviews"}, graphQLError.Path)
}

func TestGateway_AddPullRequestMetadata_empty(t *testing.T) {
	gateway := newTestGateway(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))

	require.NoError(t, gateway.AddPullRequestMetadata(
		t.Context(),
		&PullRequestMetadataInput{
			PullRequestID:   "PR_1",
			LabelIDs:        []ID{},
			ReviewerUserIDs: []ID{},
			ReviewerTeamIDs: []ID{},
			AssigneeIDs:     []ID{},
		},
	))
}

func TestGateway_AddPullRequestMetadata_combinations(t *testing.T) {
	tests := []struct {
		name  string
		input PullRequestMetadataInput
		query string
	}{
		{
			name: "Labels",
			input: PullRequestMetadataInput{
				PullRequestID: "PR_1",
				LabelIDs:      []ID{"L_1"},
			},
			query: compactGraphQL(`
				mutation($labels:AddLabelsToLabelableInput!){
					labels:addLabelsToLabelable(input: $labels){clientMutationId}
				}
			`),
		},
		{
			name: "Reviews",
			input: PullRequestMetadataInput{
				PullRequestID:   "PR_1",
				ReviewerUserIDs: []ID{"U_1"},
			},
			query: compactGraphQL(`
				mutation($reviews:RequestReviewsInput!){
					reviews:requestReviews(input: $reviews){clientMutationId}
				}
			`),
		},
		{
			name: "Assignees",
			input: PullRequestMetadataInput{
				PullRequestID: "PR_1",
				AssigneeIDs:   []ID{"U_1"},
			},
			query: compactGraphQL(`
				mutation($assignees:AddAssigneesToAssignableInput!){
					assignees:addAssigneesToAssignable(input: $assignees){clientMutationId}
				}
			`),
		},
		{
			name: "LabelsAndReviews",
			input: PullRequestMetadataInput{
				PullRequestID:   "PR_1",
				LabelIDs:        []ID{"L_1"},
				ReviewerUserIDs: []ID{"U_1"},
			},
			query: compactGraphQL(`
				mutation(
					$labels:AddLabelsToLabelableInput!
					$reviews:RequestReviewsInput!
				){
					labels:addLabelsToLabelable(input: $labels){clientMutationId},
					reviews:requestReviews(input: $reviews){clientMutationId}
				}
			`),
		},
		{
			name: "LabelsAndAssignees",
			input: PullRequestMetadataInput{
				PullRequestID: "PR_1",
				LabelIDs:      []ID{"L_1"},
				AssigneeIDs:   []ID{"U_1"},
			},
			query: compactGraphQL(`
				mutation(
					$labels:AddLabelsToLabelableInput!
					$assignees:AddAssigneesToAssignableInput!
				){
					labels:addLabelsToLabelable(input: $labels){clientMutationId},
					assignees:addAssigneesToAssignable(input: $assignees){clientMutationId}
				}
			`),
		},
		{
			name: "ReviewsAndAssignees",
			input: PullRequestMetadataInput{
				PullRequestID:   "PR_1",
				ReviewerUserIDs: []ID{"U_1"},
				AssigneeIDs:     []ID{"U_1"},
			},
			query: compactGraphQL(`
				mutation(
					$reviews:RequestReviewsInput!
					$assignees:AddAssigneesToAssignableInput!
				){
					reviews:requestReviews(input: $reviews){clientMutationId},
					assignees:addAssigneesToAssignable(input: $assignees){clientMutationId}
				}
			`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := newTestGateway(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var request struct {
					Query string `json:"query"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				assert.Equal(t, tt.query, request.Query)
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body: io.NopCloser(strings.NewReader(`{
						"data": {}
					}`)),
				}, nil
			}))

			require.NoError(t, gateway.AddPullRequestMetadata(t.Context(), &tt.input))
		})
	}
}
