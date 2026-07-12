package github

import "context"

// MergePullRequestInput is the gateway's projection of GitHub's input object.
// See https://docs.github.com/en/graphql/reference/pulls#mergepullrequestinput.
type MergePullRequestInput struct {
	// PullRequestID identifies the pull request to merge.
	PullRequestID ID `json:"pullRequestId"`

	// ClientMutationID, when non-nil, is returned unchanged by GitHub.
	ClientMutationID *string `json:"clientMutationId,omitempty"`

	// ExpectedHeadOID requires the pull request head to match when non-nil.
	ExpectedHeadOID *string `json:"expectedHeadOid,omitempty"`

	// MergeMethod is omitted when nil so GitHub selects the repository default.
	MergeMethod *MergeMethod `json:"mergeMethod,omitempty"`
}

// MergePullRequest merges a pull request.
func (c *Gateway) MergePullRequest(ctx context.Context, input *MergePullRequestInput) error {
	mutation := compactGraphQL(`
		mutation($input:MergePullRequestInput!){
			mergePullRequest(input: $input){pullRequest{id}}
		}
	`)
	return c.mutate(ctx, mutation, input, &struct{}{})
}
