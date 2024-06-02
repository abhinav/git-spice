package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

// SubmitChangeRequest is a request to submit a new change in a repository.
// The change must have already been pushed to the remote.
type SubmitChangeRequest struct {
	// Subject is the title of the change.
	Subject string // required

	// Body is the description of the change.
	Body string

	// Base is the name of the base branch
	// that this change is proposed against.
	Base string // required

	// Head is the name of the branch containing the change.
	//
	// This must have already been pushed to the remote.
	Head string // required

	// Draft specifies whether the change should be marked as a draft.
	Draft bool
}

// SubmitChangeResult is the result of creating a new change in a repository.
type SubmitChangeResult struct {
	ID  ChangeID
	URL string
}

// SubmitChange creates a new change in a repository.
func (f *Forge) SubmitChange(ctx context.Context, req SubmitChangeRequest) (SubmitChangeResult, error) {
	var m struct {
		CreatePullRequest struct {
			PullRequest struct {
				ID     githubv4.ID  `graphql:"id"`
				Number githubv4.Int `graphql:"number"`
				URL    githubv4.URI `graphql:"url"`
			} `graphql:"pullRequest"`
		} `graphql:"createPullRequest(input: $input)"`
	}

	input := githubv4.CreatePullRequestInput{
		RepositoryID: f.repoID,
		Title:        githubv4.String(req.Subject),
		BaseRefName:  githubv4.String(req.Base),
		HeadRefName:  githubv4.String(req.Head),
	}
	if req.Body != "" {
		input.Body = (*githubv4.String)(&req.Body)
	}
	if req.Draft {
		input.Draft = githubv4.NewBoolean(true)
	}

	if err := f.client.Mutate(ctx, &m, input, nil); err != nil {
		return SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	return SubmitChangeResult{
		ID:  ChangeID(m.CreatePullRequest.PullRequest.Number),
		URL: m.CreatePullRequest.PullRequest.URL.String(),
	}, nil
}
