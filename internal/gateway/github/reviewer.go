package github

import "context"

// RequestReviewsInput is the gateway's projection of GitHub's input object.
// See https://docs.github.com/en/graphql/reference/pulls#requestreviewsinput.
type RequestReviewsInput struct {
	// PullRequestID identifies the pull request whose reviewers change.
	PullRequestID ID `json:"pullRequestId"`

	// ClientMutationID, when non-nil, is returned unchanged by GitHub.
	ClientMutationID *string `json:"clientMutationId,omitempty"`

	// UserIDs is omitted when nil; an empty slice explicitly requests no users.
	UserIDs *[]ID `json:"userIds,omitempty"`

	// TeamIDs is omitted when nil; an empty slice explicitly requests no teams.
	TeamIDs *[]ID `json:"teamIds,omitempty"`

	// Union is omitted when nil; true adds to existing requests and false replaces them.
	Union *bool `json:"union,omitempty"`
}

// RequestReviews requests pull request reviews.
func (c *Gateway) RequestReviews(ctx context.Context, input *RequestReviewsInput) error {
	mutation := compactGraphQL(`
		mutation($input:RequestReviewsInput!){
			requestReviews(input: $input){clientMutationId}
		}
	`)
	return c.mutate(ctx, mutation, input, &struct{}{})
}
