package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v61/github"
)

// EditChangeOptions specifies options for an operation to edit
// an existing change.
type EditChangeOptions struct {
	// Base specifies the name of the base branch.
	//
	// If unset, the base branch is not changed.
	Base string

	// Draft specifies whether the change should be marked as a draft.
	// If unset, the draft status is not changed.
	Draft *bool
}

// EditChange edits an existing change in a repository.
func (f *Forge) EditChange(ctx context.Context, id ChangeID, opts EditChangeOptions) error {
	var req github.PullRequest
	if opts.Base != "" {
		req.Base = &github.PullRequestBranch{
			Ref: &opts.Base,
		}
	}
	if opts.Draft != nil {
		req.Draft = opts.Draft
	}

	_, _, err := f.client.PullRequests.Edit(ctx, f.owner, f.repo, int(id), &req)
	if err != nil {
		return fmt.Errorf("edit pull request: %w", err)
	}

	return nil
}
