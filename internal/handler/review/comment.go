package review

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/reviewdiff"
	"go.abhg.dev/gs/internal/spice/state"
)

// CommentRequest describes a new root review comment.
type CommentRequest struct {
	// Branch identifies the reviewed branch.
	Branch string // required

	// Anchor identifies the reviewed file or lines.
	Anchor Anchor // required

	// Message supplies the comment body without opening an editor.
	Message string
}

// SaveCommentDraft saves a root review comment for later publication.
func (h *DraftHandler) SaveCommentDraft(
	ctx context.Context,
	req *CommentRequest,
) error {
	if !req.Anchor.IsLine() {
		return errors.New("draft comments require a single-line file:line anchor")
	}

	body, err := h.commentBody(ctx, req.Message)
	if err != nil {
		return err
	}
	draft, err := h.Store.AddReviewDraft(
		ctx,
		req.Branch,
		review.Draft{ID: 0, Body: body, Anchor: req.Anchor},
	)
	if err != nil {
		return fmt.Errorf("save draft comment: %w", err)
	}

	h.Log.Infof(
		"Drafted comment %s on %s.",
		draft.ID,
		req.Anchor,
	)
	return nil
}

// PostComment immediately starts a remote review thread.
func (h *Handler) PostComment(
	ctx context.Context,
	req *CommentRequest,
) error {
	body, err := h.commentBody(ctx, req.Message)
	if err != nil {
		return err
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
	if req.Anchor.IsFile() && !patch.ContainsFile(req.Anchor.Path) {
		return fmt.Errorf(
			"review diff does not contain file %q",
			req.Anchor.Path,
		)
	}
	if !req.Anchor.IsFile() && !patch.ContainsLineRange(
		req.Anchor.Path,
		req.Anchor.StartLine,
		req.Anchor.EndLine,
	) {
		return fmt.Errorf(
			"review diff does not contain %s",
			req.Anchor,
		)
	}

	return h.postComment(
		ctx,
		change.Change.ChangeID(),
		forge.SubmitReviewCommentRequest{
			Path: req.Anchor.Path,
			Range: forge.ReviewThreadRange{
				StartLine: req.Anchor.StartLine,
				EndLine:   req.Anchor.EndLine,
			},
			Body: body,
			Side: forge.ReviewThreadSideRight,
		},
	)
}

// ReplyRequest describes a reply to an existing review thread.
type ReplyRequest struct {
	// Branch identifies the reviewed branch.
	Branch string // required

	// ThreadID is the command-line representation of the forge thread ID.
	ThreadID string // required

	// Message supplies the reply body without opening an editor.
	Message string
}

// SaveReplyDraft saves a review-thread reply for later publication.
func (h *DraftHandler) SaveReplyDraft(
	ctx context.Context,
	req *ReplyRequest,
) error {
	body, err := h.commentBody(ctx, req.Message)
	if err != nil {
		return err
	}
	draft, err := h.Store.AddReviewDraft(
		ctx,
		req.Branch,
		review.Draft{ID: 0, Body: body, ReplyTo: req.ThreadID},
	)
	if err != nil {
		return fmt.Errorf("save draft reply: %w", err)
	}

	h.Log.Infof(
		"Drafted reply %s to thread %s.",
		draft.ID,
		req.ThreadID,
	)
	return nil
}

// PostReply immediately appends a reply to a remote review thread.
func (h *Handler) PostReply(
	ctx context.Context,
	req *ReplyRequest,
) error {
	body, err := h.commentBody(ctx, req.Message)
	if err != nil {
		return err
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

	// ReviewThreadID is opaque.
	// Recover the forge-owned value whose String form the command accepted.
	var threadID forge.ReviewThreadID
	for thread, err := range h.Repository.ListReviewThreads(
		ctx,
		change.Change.ChangeID(),
	) {
		if err != nil {
			return fmt.Errorf("list review threads: %w", err)
		}
		if thread.ID.String() == req.ThreadID {
			threadID = thread.ID
			break
		}
	}
	if threadID == nil {
		return fmt.Errorf("review thread %q not found", req.ThreadID)
	}

	return h.postComment(
		ctx,
		change.Change.ChangeID(),
		forge.SubmitReviewCommentRequest{
			Body:    body,
			ReplyTo: threadID,
		},
	)
}

// commentBody returns a supplied comment body or opens an empty editor.
// Whitespace-only input is rejected before any draft or forge mutation.
func (h *Handler) commentBody(ctx context.Context, body string) (string, error) {
	if body == "" {
		var err error
		body, err = h.Editor(ctx, "")
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New("empty comment body, aborting")
	}
	return body, nil
}

// commentBody returns a supplied draft body or opens an empty editor.
// Whitespace-only input is rejected before the draft is persisted.
func (h *DraftHandler) commentBody(
	ctx context.Context,
	body string,
) (string, error) {
	if body == "" {
		var err error
		body, err = h.Editor(ctx, "")
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New("empty comment body, aborting")
	}
	return body, nil
}

// loadPatch parses the selected branch's review diff.
// Closing the diff reader also reports failures from the Git process.
func (h *Handler) loadPatch(
	ctx context.Context,
	base, branch string,
) (*reviewdiff.Patch, error) {
	diff, err := h.Worktree.OpenBranchDiff(ctx, base, branch)
	if err != nil {
		return nil, fmt.Errorf("open diff: %w", err)
	}
	patch, err := reviewdiff.Parse(diff)
	err = errors.Join(err, diff.Close())
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	return patch, nil
}

// postComment submits one comment-only review and reports its new thread ID.
func (h *Handler) postComment(
	ctx context.Context,
	changeID forge.ChangeID,
	comment forge.SubmitReviewCommentRequest,
) error {
	result, err := h.Repository.SubmitReview(
		ctx,
		changeID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{comment},
		},
	)
	if err != nil {
		return fmt.Errorf("post review comment: %w", err)
	}
	if len(result.Comments) != 1 {
		return fmt.Errorf(
			"post review comment: forge returned %d comment results",
			len(result.Comments),
		)
	}

	h.Log.Infof(
		"Posted comment %s on %s.",
		result.Comments[0].ThreadID.String(),
		changeID,
	)
	return nil
}
