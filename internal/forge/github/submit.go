package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v61/github"
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
	pr, _, err := f.client.PullRequests.Create(ctx, f.owner, f.repo, &github.NewPullRequest{
		Title: &req.Subject,
		Body:  &req.Body,
		Base:  &req.Base,
		Head:  &req.Head,
		Draft: &req.Draft,
	})
	if err != nil {
		return SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	return SubmitChangeResult{
		ID:  ChangeID(pr.GetNumber()),
		URL: pr.GetHTMLURL(),
	}, nil
}
