package github

import "context"

// CreatePullRequestInput is the gateway's projection of GitHub's input object.
// See https://docs.github.com/en/graphql/reference/pulls#createpullrequestinput.
type CreatePullRequestInput struct {
	// RepositoryID identifies the repository where GitHub creates the pull request.
	RepositoryID ID `json:"repositoryId"`

	// BaseRefName is the target branch name.
	BaseRefName string `json:"baseRefName"`

	// HeadRefName is the source branch name.
	HeadRefName string `json:"headRefName"`

	// Title is the pull request title.
	Title string `json:"title"`

	// ClientMutationID, when non-nil, is returned unchanged by GitHub.
	ClientMutationID *string `json:"clientMutationId,omitempty"`

	// HeadRepositoryID identifies the fork containing HeadRefName.
	HeadRepositoryID *ID `json:"headRepositoryId,omitempty"`

	// Body is omitted when nil; an empty string clears the body.
	Body *string `json:"body,omitempty"`

	// Draft is omitted when nil; false explicitly requests a ready pull request.
	Draft *bool `json:"draft,omitempty"`
}

// CreatedPullRequest identifies a pull request returned after creation.
type CreatedPullRequest struct {
	// ID is the new pull request's GraphQL node ID.
	ID ID `json:"id"`

	// Number is the repository-local pull request number.
	Number int `json:"number"`

	// URL is GitHub's browser URL for the pull request.
	URL string `json:"url"`
}

// CreatePullRequest creates a pull request.
func (c *Gateway) CreatePullRequest(ctx context.Context, input *CreatePullRequestInput) (*CreatedPullRequest, error) {
	var result struct {
		CreatePullRequest struct {
			PullRequest *CreatedPullRequest `json:"pullRequest"`
		} `json:"createPullRequest"`
	}
	mutation := compactGraphQL(`
		mutation($input:CreatePullRequestInput!){
			createPullRequest(input: $input){
				pullRequest{id,number,url}
			}
		}
	`)
	if err := c.mutate(ctx, mutation, input, &result); err != nil {
		return nil, err
	}
	return result.CreatePullRequest.PullRequest, nil
}
