package review

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// LoadRequest selects review comments to load.
type LoadRequest struct {
	// Branch identifies the reviewed branch.
	// The current branch is used when Branch is empty.
	Branch string

	// DraftOnly omits comments already published to the forge.
	DraftOnly bool

	// Unresolved omits resolved forge threads.
	Unresolved bool
}

// LoadResult contains local drafts and published review comments.
type LoadResult struct {
	// Branch is the branch selected by the request.
	Branch string

	// Drafts are the branch's unpublished comments.
	Drafts []Draft

	// Comments are published comments paired with their owning thread.
	Comments []ListedComment
}

// ListedComment pairs a review comment with its thread location and state.
type ListedComment struct {
	// Thread owns the comment's location and resolution state.
	Thread forge.ReviewThread

	// Comment is one entry in Thread.
	Comment forge.ReviewComment
}

// LoadReviewData loads local drafts and remote review comments for a branch.
func (h *Handler) LoadReviewData(
	ctx context.Context,
	req *LoadRequest,
) (*LoadResult, error) {
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return nil, err
	}

	drafts, err := h.Store.LoadReviewDrafts(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("load draft comments: %w", err)
	}
	result := &LoadResult{
		Branch: branch,
		Drafts: drafts,
	}
	if req.DraftOnly {
		return result, nil
	}

	change, err := lookupBranch(ctx, h.Service, branch)
	if err != nil {
		return nil, err
	}
	if change.Change == nil {
		h.Log.Infof("No change request found for %s.", branch)
		return result, nil
	}

	// Keep each comment beside its thread while consuming the forge iterator.
	// The presenter needs thread-level coordinates and state for every comment.
	for thread, err := range h.Repository.ListReviewThreads(
		ctx,
		change.Change.ChangeID(),
	) {
		if err != nil {
			return nil, fmt.Errorf("list review threads: %w", err)
		}
		if req.Unresolved && thread.Resolved != nil && *thread.Resolved {
			continue
		}
		for _, comment := range thread.Comments {
			result.Comments = append(result.Comments, ListedComment{
				Thread:  *thread,
				Comment: comment,
			})
		}
	}
	return result, nil
}
