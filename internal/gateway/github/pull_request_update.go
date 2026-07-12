package github

import "context"

// UpdatePullRequestInput is the gateway's projection of GitHub's input object.
// See https://docs.github.com/en/graphql/reference/pulls#updatepullrequestinput.
type UpdatePullRequestInput struct {
	// PullRequestID identifies the pull request to update.
	PullRequestID ID `json:"pullRequestId"`

	// ClientMutationID, when non-nil, is returned unchanged by GitHub.
	ClientMutationID *string `json:"clientMutationId,omitempty"`

	// BaseRefName is omitted when nil; an empty value is sent to GitHub.
	BaseRefName *string `json:"baseRefName,omitempty"`
}

// UpdatePullRequest updates pull request fields.
func (c *Gateway) UpdatePullRequest(ctx context.Context, input *UpdatePullRequestInput) error {
	mutation := compactGraphQL(`
		mutation($input:UpdatePullRequestInput!){
			updatePullRequest(input: $input){clientMutationId}
		}
	`)
	return c.mutate(ctx, mutation, input, &struct{}{})
}

// ClosePullRequest closes a pull request.
func (c *Gateway) ClosePullRequest(ctx context.Context, pullRequestID ID) error {
	mutation := compactGraphQL(`
		mutation($input:UpdatePullRequestInput!){
			updatePullRequest(input: $input){pullRequest{id}}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		PullRequestID ID     `json:"pullRequestId"`
		State         string `json:"state"`
	}{pullRequestID, "CLOSED"}, &struct{}{})
}

// ConvertPullRequestToDraft converts a pull request to draft state.
func (c *Gateway) ConvertPullRequestToDraft(ctx context.Context, pullRequestID ID) error {
	mutation := compactGraphQL(`
		mutation($input:ConvertPullRequestToDraftInput!){
			convertPullRequestToDraft(input: $input){pullRequest{id}}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		PullRequestID ID `json:"pullRequestId"`
	}{pullRequestID}, &struct{}{})
}

// MarkPullRequestReadyForReview marks a draft pull request ready for review.
func (c *Gateway) MarkPullRequestReadyForReview(ctx context.Context, pullRequestID ID) error {
	mutation := compactGraphQL(`
		mutation($input:MarkPullRequestReadyForReviewInput!){
			markPullRequestReadyForReview(input: $input){pullRequest{id}}
		}
	`)
	return c.mutate(ctx, mutation, struct {
		PullRequestID ID `json:"pullRequestId"`
	}{pullRequestID}, &struct{}{})
}
