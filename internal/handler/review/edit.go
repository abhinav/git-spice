package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ReplaceDraftBodyRequest identifies a local draft and its new body.
type ReplaceDraftBodyRequest struct {
	// Branch identifies the branch containing the draft.
	Branch string // required

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
	drafts, err := h.Store.LoadReviewDrafts(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("load draft comments: %w", err)
	}
	idx := slices.IndexFunc(drafts, func(draft Draft) bool {
		return draft.ID == req.ID
	})
	if idx < 0 {
		return fmt.Errorf("draft comment %d not found", req.ID)
	}

	body := req.Message
	if body == "" {
		// Seed the editor with the current body so the user can revise it.
		body, err = h.Editor(ctx, drafts[idx].Body)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment body, aborting")
	}
	if err := h.Store.UpdateReviewDraftBody(
		ctx,
		req.Branch,
		req.ID,
		body,
	); err != nil {
		return fmt.Errorf("save draft comment: %w", err)
	}

	h.Log.Infof("Updated draft comment %d.", req.ID)
	return nil
}
