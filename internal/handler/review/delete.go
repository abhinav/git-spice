package review

import (
	"context"
	"fmt"
)

// DeleteDraftsRequest describes local review drafts to delete.
type DeleteDraftsRequest struct {
	// Branch identifies the branch containing the drafts.
	Branch string // required

	// IDs identify drafts within the branch.
	IDs []DraftID // required
}

// DeleteDrafts removes local review drafts.
func (h *DraftHandler) DeleteDrafts(
	ctx context.Context,
	req *DeleteDraftsRequest,
) error {
	deleted, err := h.Store.DeleteReviewDrafts(ctx, req.Branch, req.IDs)
	if err != nil {
		return fmt.Errorf("delete draft comments: %w", err)
	}
	h.Log.Infof("Deleted %d draft comment(s).", deleted)
	return nil
}
