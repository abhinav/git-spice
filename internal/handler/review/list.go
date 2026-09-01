package review

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice/state"
)

// LoadRequest selects review comments to load.
type LoadRequest struct {
	// Branch identifies the reviewed branch.
	Branch string // required

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
	drafts, err := h.Store.LoadReviewDrafts(ctx, req.Branch)
	if err != nil {
		return nil, fmt.Errorf("load draft comments: %w", err)
	}
	result := &LoadResult{
		Branch: req.Branch,
		Drafts: drafts,
	}
	if req.DraftOnly {
		return result, nil
	}

	change, err := h.Service.LookupBranch(ctx, req.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return nil, fmt.Errorf("branch not tracked: %s", req.Branch)
		}
		return nil, fmt.Errorf("get branch: %w", err)
	}
	if change.Change == nil {
		h.Log.Infof("No change request found for %s.", req.Branch)
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
