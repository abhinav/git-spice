package review

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/review"
	"go.abhg.dev/gs/internal/reviewdiff"
)

// CommentRequest describes a new root review comment.
type CommentRequest struct {
	// Branch identifies the reviewed branch.
	// The current branch is used when Branch is empty.
	Branch string

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
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}
	if !req.Anchor.IsLine() {
		return errors.New("draft comments require a single-line file:line anchor")
	}

	body, err := editCommentBody(ctx, h.Editor, req.Message, "")
	if err != nil {
		return err
	}
	draft, err := h.Store.AddReviewDraft(
		ctx,
		branch,
		review.NewCommentDraft(0, req.Anchor, body),
	)
	if err != nil {
		return fmt.Errorf("save draft comment: %w", err)
	}

	h.Log.Infof(
		"Drafted comment %s on %s.",
		draft.ID(),
		req.Anchor,
	)
	return nil
}

// PostComment immediately starts a remote review thread.
func (h *Handler) PostComment(
	ctx context.Context,
	req *CommentRequest,
) error {
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}
	body, err := editCommentBody(ctx, h.Editor, req.Message, "")
	if err != nil {
		return err
	}

	change, err := lookupReviewChange(ctx, h.Service, branch)
	if err != nil {
		return err
	}
	patch, err := h.loadPatch(ctx, change.Base, branch)
	if err != nil {
		return err
	}
	if req.Anchor.IsFile() && !patch.ContainsFile(req.Anchor.Path()) {
		return fmt.Errorf(
			"review diff does not contain file %q",
			req.Anchor.Path(),
		)
	}
	if !req.Anchor.IsFile() && !patch.ContainsLineRange(
		req.Anchor.Path(),
		req.Anchor.StartLine(),
		req.Anchor.EndLine(),
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
			Path:  req.Anchor.Path(),
			Range: forgeRange(req.Anchor),
			Body:  body,
			Side:  forge.ReviewThreadSideRight,
		},
	)
}

// ReplyRequest describes a reply to an existing review thread.
type ReplyRequest struct {
	// Branch identifies the reviewed branch.
	// The current branch is used when Branch is empty.
	Branch string

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
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}
	body, err := editCommentBody(ctx, h.Editor, req.Message, "")
	if err != nil {
		return err
	}
	draft, err := h.Store.AddReviewDraft(
		ctx,
		branch,
		review.NewReplyDraft(0, req.ThreadID, body),
	)
	if err != nil {
		return fmt.Errorf("save draft reply: %w", err)
	}

	h.Log.Infof(
		"Drafted reply %s to thread %s.",
		draft.ID(),
		req.ThreadID,
	)
	return nil
}

// PostReply immediately appends a reply to a remote review thread.
func (h *Handler) PostReply(
	ctx context.Context,
	req *ReplyRequest,
) error {
	branch, err := resolveBranch(ctx, h.Worktree, req.Branch)
	if err != nil {
		return err
	}
	body, err := editCommentBody(ctx, h.Editor, req.Message, "")
	if err != nil {
		return err
	}
	change, err := lookupReviewChange(ctx, h.Service, branch)
	if err != nil {
		return err
	}
	threadID, err := findReviewThreadID(
		ctx,
		h.Repository,
		change.Change.ChangeID(),
		req.ThreadID,
	)
	if err != nil {
		return err
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

func editCommentBody(
	ctx context.Context,
	editor CommentEditor,
	message string,
	initial string,
) (string, error) {
	body := message
	if body == "" {
		var err error
		body, err = editor(ctx, initial)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New("empty comment body, aborting")
	}
	return body, nil
}

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

func forgeRange(anchor Anchor) forge.ReviewThreadRange {
	return forge.ReviewThreadRange{
		StartLine: anchor.StartLine(),
		EndLine:   anchor.EndLine(),
	}
}
