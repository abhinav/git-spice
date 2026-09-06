package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

type reviewCmd struct {
	Comment reviewCommentCmd `cmd:"" help:"Draft or post a review comment"`
	Reply   reviewReplyCmd   `cmd:"" help:"Draft or post a reply to a review thread"`
	Publish reviewPublishCmd `cmd:"" help:"Publish draft comments as a review"`
	List    reviewListCmd    `cmd:"" aliases:"ls" help:"List review comments"`
	Edit    reviewEditCmd    `cmd:"" help:"Edit a draft comment"`
	Resolve reviewResolveCmd `cmd:"" help:"Resolve a review thread"`
	Reopen  reviewReopenCmd  `cmd:"" help:"Reopen a resolved review thread"`
}

// reviewRepositoryForBranch resolves the change and review-capable repository
// shared by review commands that operate on remote state.
func reviewRepositoryForBranch(
	ctx context.Context,
	svc *spice.Service,
	forgeRepo forge.Repository,
	branch string,
) (*spice.LookupBranchResponse, forge.ReviewRepository, error) {
	b, err := svc.LookupBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil, nil, fmt.Errorf("branch not tracked: %s", branch)
		}
		return nil, nil, fmt.Errorf("get branch: %w", err)
	}
	if b.Change == nil {
		return nil, nil, fmt.Errorf(
			"no change request for %s; "+
				"submit the branch first with "+
				"'gs branch submit'",
			branch,
		)
	}

	reviewRepo, ok := forgeRepo.(forge.ReviewRepository)
	if !ok {
		return nil, nil, errors.New(
			"forge does not support review comments",
		)
	}
	return b, reviewRepo, nil
}

// loadReviewThreadIDs indexes the forge's native thread identifiers by their
// command-line representation. ReviewThreadID is intentionally opaque, so a
// command must recover the provider-owned value before replying to or resolving
// a thread named by the user.
func loadReviewThreadIDs(
	ctx context.Context,
	repo forge.ReviewRepository,
	changeID forge.ChangeID,
) (map[string]forge.ReviewThreadID, error) {
	ids := make(map[string]forge.ReviewThreadID)
	for thread, err := range repo.ListReviewThreads(ctx, changeID) {
		if err != nil {
			return nil, fmt.Errorf("list review threads: %w", err)
		}
		ids[thread.ID.String()] = thread.ID
	}
	return ids, nil
}

// reviewThreadID resolves a user-supplied thread string to the opaque ID value
// returned by the current forge.
func reviewThreadID(
	ids map[string]forge.ReviewThreadID,
	id string,
) (forge.ReviewThreadID, error) {
	threadID, ok := ids[id]
	if !ok {
		return nil, fmt.Errorf("review thread %q not found", id)
	}
	return threadID, nil
}
