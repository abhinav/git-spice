package review

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// PublishDraftsRequest describes a review assembled from local drafts.
type PublishDraftsRequest struct {
	// Branch identifies the branch whose drafts will be published.
	// The current branch is used when Branch is empty.
	Branch string

	// Body is the optional overall review body.
	Body string

	// Disposition is the optional review outcome to publish.
	Disposition forge.ReviewDisposition
}

// PublishDrafts submits every local draft for a branch as one review.
func (h *Handler) PublishDrafts(
	ctx context.Context,
	req *PublishDraftsRequest,
) error {
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}

	drafts, err := h.Store.LoadReviewDrafts(ctx, branch)
	if err != nil {
		return fmt.Errorf("load draft comments: %w", err)
	}
	if len(drafts) == 0 {
		h.Log.Infof("No draft comments to publish.")
		return nil
	}

	change, err := lookupReviewChange(ctx, h.Service, branch)
	if err != nil {
		return err
	}
	patch, err := h.loadPatch(ctx, change.Base, branch)
	if err != nil {
		return err
	}
	threadIDs, err := resolveDraftThreadIDs(
		ctx,
		h.Repository,
		change.Change.ChangeID(),
		drafts,
	)
	if err != nil {
		return err
	}

	comments := make([]forge.SubmitReviewCommentRequest, 0, len(drafts))
	for _, draft := range drafts {
		if replyTo, ok := draft.ReplyTo(); ok {
			comments = append(comments, forge.SubmitReviewCommentRequest{
				Body:    draft.Body(),
				ReplyTo: threadIDs[replyTo],
			})
			continue
		}

		anchor, _ := draft.Anchor()
		if !patch.ContainsLineRange(
			anchor.Path(),
			anchor.StartLine(),
			anchor.EndLine(),
		) {
			return fmt.Errorf(
				"draft %s: review diff does not contain %s",
				draft.ID(),
				anchor,
			)
		}
		comments = append(comments, forge.SubmitReviewCommentRequest{
			Path:  anchor.Path(),
			Range: forgeRange(anchor),
			Body:  draft.Body(),
			Side:  forge.ReviewThreadSideRight,
		})
	}

	if _, err := h.Repository.SubmitReview(
		ctx,
		change.Change.ChangeID(),
		forge.SubmitReviewRequest{
			Body:        req.Body,
			Disposition: req.Disposition,
			Comments:    comments,
		},
	); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}
	if err := h.Store.ClearReviewDrafts(ctx, branch); err != nil {
		return fmt.Errorf("clear draft comments: %w", err)
	}

	h.Log.Infof(
		"Published %d comment(s) as review on %s.",
		len(comments),
		change.Change.ChangeID(),
	)
	return nil
}

func resolveDraftThreadIDs(
	ctx context.Context,
	repository forge.ReviewRepository,
	changeID forge.ChangeID,
	drafts []Draft,
) (map[string]forge.ReviewThreadID, error) {
	wanted := make(map[string]struct{})
	for _, draft := range drafts {
		if replyTo, ok := draft.ReplyTo(); ok {
			wanted[replyTo] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	resolved := make(map[string]forge.ReviewThreadID, len(wanted))
	// Drafts persist the String form of opaque ReviewThreadIDs.
	// Resolve every reply target in one traversal before submission.
	for thread, err := range repository.ListReviewThreads(ctx, changeID) {
		if err != nil {
			return nil, fmt.Errorf("list review threads: %w", err)
		}
		id := thread.ID.String()
		if _, ok := wanted[id]; ok {
			resolved[id] = thread.ID
			delete(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return resolved, nil
	}

	for _, draft := range drafts {
		replyTo, isReply := draft.ReplyTo()
		if !isReply {
			continue
		}
		if _, ok := wanted[replyTo]; ok {
			return nil, fmt.Errorf(
				"draft %s: review thread %q not found",
				draft.ID(),
				replyTo,
			)
		}
	}
	panic("unresolved thread ID was not sourced from a draft")
}
