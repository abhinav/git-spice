package review

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice/state"
)

// PublishDraftsRequest describes a review assembled from local drafts.
type PublishDraftsRequest struct {
	// Branch identifies the branch whose drafts will be published.
	Branch string // required

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
	drafts, err := h.Store.LoadReviewDrafts(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("load draft comments: %w", err)
	}
	if len(drafts) == 0 {
		h.Log.Infof("No draft comments to publish.")
		return nil
	}

	change, err := h.Service.LookupBranch(ctx, req.Branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", req.Branch)
		}
		return fmt.Errorf("get branch: %w", err)
	}
	if change.Change == nil {
		return fmt.Errorf(
			"no change request for %s; "+
				"submit the branch first with "+
				"'gs branch submit'",
			req.Branch,
		)
	}
	patch, err := h.loadPatch(ctx, change.Base, req.Branch)
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
		if draft.ReplyTo != "" {
			comments = append(comments, forge.SubmitReviewCommentRequest{
				Body:    draft.Body,
				ReplyTo: threadIDs[draft.ReplyTo],
			})
			continue
		}

		if !patch.ContainsLineRange(
			draft.Anchor.Path,
			draft.Anchor.StartLine,
			draft.Anchor.EndLine,
		) {
			return fmt.Errorf(
				"draft %s: review diff does not contain %s",
				draft.ID,
				draft.Anchor,
			)
		}
		comments = append(comments, forge.SubmitReviewCommentRequest{
			Path: draft.Anchor.Path,
			Range: forge.ReviewThreadRange{
				StartLine: draft.Anchor.StartLine,
				EndLine:   draft.Anchor.EndLine,
			},
			Body: draft.Body,
			Side: forge.ReviewThreadSideRight,
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
	if err := h.Store.ClearReviewDrafts(ctx, req.Branch); err != nil {
		return fmt.Errorf("clear draft comments: %w", err)
	}

	h.Log.Infof(
		"Published %d comment(s) as review on %s.",
		len(comments),
		change.Change.ChangeID(),
	)
	return nil
}

// resolveDraftThreadIDs recovers opaque forge IDs for every drafted reply.
// One traversal resolves all targets before the review is submitted.
func resolveDraftThreadIDs(
	ctx context.Context,
	repository forge.ReviewRepository,
	changeID forge.ChangeID,
	drafts []Draft,
) (map[string]forge.ReviewThreadID, error) {
	wanted := make(map[string]struct{})
	for _, draft := range drafts {
		if draft.ReplyTo != "" {
			wanted[draft.ReplyTo] = struct{}{}
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
		if draft.ReplyTo == "" {
			continue
		}
		if _, ok := wanted[draft.ReplyTo]; ok {
			return nil, fmt.Errorf(
				"draft %s: review thread %q not found",
				draft.ID,
				draft.ReplyTo,
			)
		}
	}
	panic("unresolved thread ID was not sourced from a draft")
}
