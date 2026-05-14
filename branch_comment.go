package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

type branchCommentCmd struct {
	List         branchCommentListCmd         `cmd:"" aliases:"ls" help:"List comments on a change request"`
	Stage        branchCommentStageCmd        `cmd:"" help:"Stage an inline comment for batch submission"`
	Add          branchCommentAddCmd          `cmd:"" help:"Post an inline comment immediately"`
	SubmitStaged branchCommentSubmitStagedCmd `cmd:"" aliases:"ss" help:"Submit all staged comments as a review"`
	Resolve      branchCommentResolveCmd      `cmd:"" help:"Resolve or unresolve a review thread"`
	Edit         branchCommentEditCmd         `cmd:"" help:"Edit a comment"`
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

// reviewThreadSide translates diffmap's textual side into the shared review
// model used by forge implementations.
func reviewThreadSide(side string) (forge.ReviewThreadSide, error) {
	switch side {
	case "RIGHT":
		return forge.ReviewThreadSideRight, nil
	case "LEFT":
		return forge.ReviewThreadSideLeft, nil
	default:
		return 0, fmt.Errorf("unknown review thread side %q", side)
	}
}
