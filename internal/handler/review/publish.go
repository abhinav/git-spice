package review

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/reviewdiff"
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
	changeID := change.Change.ChangeID()
	reviewHead := change.Head
	// Prefer the remote change head because forges interpret review coordinates
	// against it. Status lookup is best-effort so review publication retains its
	// pre-remapping behavior when the forge cannot report that revision.
	statuses, err := h.Repository.ChangeStatuses(ctx, []forge.ChangeID{changeID})
	if err != nil {
		h.Log.Warnf("%v: remote head unavailable, using local branch head (%v): %v", changeID, reviewHead.Short(), err)
	} else {
		must.BeEqualf(len(statuses), 1, "expected one status for %v, got %d", changeID, len(statuses))
		if statuses[0].HeadHash.IsZero() {
			h.Log.Warnf("%v: remote head unavailable, using local branch head (%v)", changeID, reviewHead.Short())
		} else {
			reviewHead = statuses[0].HeadHash
		}
	}

	patchHead := req.Branch
	resolvedHead, err := h.Worktree.PeelToCommit(ctx, reviewHead.String())
	if err != nil {
		h.Log.Warnf("%v: head (%v) unavailable locally, using saved anchors: %v", changeID, reviewHead.Short(), err)
	} else {
		patchHead = resolvedHead.String()
		if err := h.remapDraftAnchors(ctx, resolvedHead, drafts); err != nil {
			return err
		}
	}

	patch, err := h.loadPatch(ctx, change.Base, patchHead)
	if err != nil {
		return err
	}
	threadIDs, err := resolveDraftThreadIDs(
		ctx,
		h.Repository,
		changeID,
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

		anchor := draft.Anchor
		if anchor.IsFile() && !patch.ContainsFile(anchor.Path) {
			return fmt.Errorf(
				"draft %s: review diff does not contain file %q",
				draft.ID,
				anchor.Path,
			)
		}
		if !anchor.IsFile() && !patch.ContainsLineRange(anchor.Path, anchor.StartLine, anchor.EndLine) {
			return fmt.Errorf(
				"draft %s: review diff does not contain %s",
				draft.ID,
				anchor,
			)
		}
		comments = append(comments, forge.SubmitReviewCommentRequest{
			Path: anchor.Path,
			Range: forge.ReviewThreadRange{
				StartLine: anchor.StartLine,
				EndLine:   anchor.EndLine,
			},
			Body: draft.Body,
			Side: forge.ReviewThreadSideRight,
		})
	}

	if _, err := h.Repository.SubmitReview(
		ctx,
		changeID,
		forge.SubmitReviewRequest{
			Body:        req.Body,
			Disposition: req.Disposition,
			Comments:    comments,
		},
	); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}
	if err := h.Store.RemovePublishedReviewDrafts(
		ctx,
		req.Branch,
		drafts,
	); err != nil {
		return fmt.Errorf("remove published draft comments: %w", err)
	}

	h.Log.Infof(
		"Published %d comment(s) as review on %s.",
		len(comments),
		changeID,
	)
	return nil
}

// remapDraftAnchors follows root drafts from their recorded branch revisions
// to head. Failures loading an individual source patch preserve saved
// coordinates; a target known to have been deleted returns an error.
func (h *Handler) remapDraftAnchors(
	ctx context.Context,
	head git.Hash,
	drafts []review.Draft,
) error {
	// draftAnchorSource memoizes one source-to-head patch or its load failure.
	type draftAnchorSource struct {
		patch *reviewdiff.Patch
		err   error
	}

	// Key entries by the commit hash recorded with each draft. Drafts created
	// at the same branch revision then share a patch or its load failure.
	sources := make(map[git.Hash]draftAnchorSource)
	for idx, draft := range drafts {
		if draft.ReplyTo != "" {
			continue
		}

		if draft.CommitHash.IsZero() || draft.CommitHash == head {
			// Unlikely that the commit hash wasn't recorded
			// but easy enough to handle.
			continue
		}

		source, ok := sources[draft.CommitHash]
		if !ok {
			source.patch, source.err = h.loadDraftAnchorPatch(ctx, draft.CommitHash, head)
			sources[draft.CommitHash] = source
		}
		if source.err != nil {
			h.Log.Warn(
				"Draft head moved but patch was unavailable. Using saved anchor.",
				"draftID", draft.ID,
				"anchor", draft.Anchor,
				"oldHead", draft.CommitHash.Short(),
				"newHead", head.Short(),
				"error", source.err,
			)
			continue
		}

		anchor, ok := source.patch.MapAnchor(draft.Anchor)
		if !ok {
			return fmt.Errorf(
				"draft %s: comment target %s no longer exists after branch changes",
				draft.ID,
				draft.Anchor,
			)
		}

		if anchor != draft.Anchor {
			h.Log.Infof("Draft %d: anchor moved to %v", draft.ID, anchor)
		}

		draft.Anchor = anchor
		drafts[idx] = draft
	}
	return nil
}

// loadDraftAnchorPatch parses the change from the commit hash recorded with a
// draft to the branch's current head.
func (h *Handler) loadDraftAnchorPatch(
	ctx context.Context,
	draftCommit git.Hash,
	head git.Hash,
) (*reviewdiff.Patch, error) {
	source, err := h.Worktree.PeelToCommit(ctx, draftCommit.String())
	if err != nil {
		return nil, fmt.Errorf(
			"resolve source commit %s: %w",
			draftCommit.Short(),
			err,
		)
	}

	diff, err := h.Worktree.OpenCommitDiff(
		ctx,
		source.String(),
		head.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("open commit diff: %w", err)
	}
	patch, err := parsePatch(diff)
	if err != nil {
		return nil, fmt.Errorf("parse commit diff: %w", err)
	}
	return patch, nil
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
