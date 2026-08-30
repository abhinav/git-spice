package review

import (
	"context"
	"fmt"
	"slices"
)

// ReplaceDraftBodyRequest identifies a local draft and its new body.
type ReplaceDraftBodyRequest struct {
	// Branch identifies the branch containing the draft.
	// The current branch is used when Branch is empty.
	Branch string

	// ID identifies the draft within the branch.
	ID DraftID // required

	// Message supplies the new body without opening an editor.
	Message string
}

// ReplaceDraftBody edits the body of a local review draft.
func (h *DraftHandler) ReplaceDraftBody(
	ctx context.Context,
	req *ReplaceDraftBodyRequest,
) error {
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}

	drafts, err := h.Store.LoadReviewDrafts(ctx, branch)
	if err != nil {
		return fmt.Errorf("load draft comments: %w", err)
	}
	idx := slices.IndexFunc(drafts, func(draft Draft) bool {
		return draft.ID() == req.ID
	})
	if idx < 0 {
		return fmt.Errorf("draft comment %d not found", req.ID)
	}

	body, err := editCommentBody(
		ctx,
		h.Editor,
		req.Message,
		drafts[idx].Body(),
	)
	if err != nil {
		return err
	}
	if err := h.Store.UpdateReviewDraftBody(
		ctx,
		branch,
		req.ID,
		body,
	); err != nil {
		return fmt.Errorf("save draft comment: %w", err)
	}

	h.Log.Infof("Updated draft comment %d.", req.ID)
	return nil
}
