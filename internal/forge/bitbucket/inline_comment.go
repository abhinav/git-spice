package bitbucket

import (
	"context"
	"fmt"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

var _ forge.WithInlineComments = (*Repository)(nil)

// ListInlineComments lists inline/review comments on a PR
// by filtering comments that have inline position data.
func (r *Repository) ListInlineComments(
	ctx context.Context,
	id forge.ChangeID,
) ([]*forge.InlineComment, error) {
	prID := mustPR(id).Number

	var comments []*forge.InlineComment
	opts := &bitbucket.CommentListOptions{}
	for {
		page, _, err := r.client.CommentList(
			ctx, r.workspace, r.repo, prID, opts,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"list comments: %w", err,
			)
		}

		for _, c := range page.Values {
			if c.Inline == nil {
				continue
			}

			line := 0
			if c.Inline.To != nil {
				line = *c.Inline.To
			}

			// Thread ID encodes comment ID and PR ID
			// for resolve/unresolve operations.
			id := formatInlineCommentThreadID(c.ID, prID)

			comments = append(comments,
				&forge.InlineComment{
					ID: id,
					CommentID: &PRComment{
						ID:   c.ID,
						PRID: prID,
					},
					Path:     c.Inline.Path,
					Lines:    forge.InlineCommentLine(line),
					Body:     c.Content.Raw,
					Author:   c.User.DisplayName,
					Resolved: c.Resolution != nil,
				})
		}

		if page.Next == "" {
			break
		}
		opts.PageURL = page.Next
	}

	return comments, nil
}

// SubmitReview posts a batch of inline comments
// as individual comments on a PR.
// Bitbucket does not have a native batch review API,
// so each comment is posted separately.
func (r *Repository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.ReviewRequest,
) error {
	prID := mustPR(id).Number

	for _, c := range req.Comments {
		if err := r.submitOneComment(
			ctx, prID, c,
		); err != nil {
			return err
		}
	}

	r.log.Debug("Submitted review",
		"pr", prID,
		"comments", len(req.Comments),
	)
	return nil
}

func (r *Repository) submitOneComment(
	ctx context.Context,
	prID int64,
	c forge.InlineCommentRequest,
) error {
	if c.InReplyTo != "" {
		parentID, _, err := parseInlineCommentThreadID(c.InReplyTo)
		if err != nil {
			return fmt.Errorf("parse inline comment ID: %w", err)
		}
		_, err = r.replyToComment(
			ctx, prID, parentID, c.Body,
		)
		return err
	}
	_, err := r.postInlineCommentAPI(
		ctx, prID, c.Path, c.Lines, c.Body,
	)
	return err
}

// PostInlineComment posts a single inline comment on a PR.
// If req.InReplyTo is set, posts a reply to that thread
// instead of creating a new inline comment.
func (r *Repository) PostInlineComment(
	ctx context.Context,
	id forge.ChangeID,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	prID := mustPR(id).Number

	if req.InReplyTo != "" {
		return r.postReply(ctx, prID, req)
	}
	return r.postNewInlineComment(ctx, prID, req)
}

func (r *Repository) postNewInlineComment(
	ctx context.Context,
	prID int64,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	comment, err := r.postInlineCommentAPI(
		ctx, prID, req.Path, req.Lines, req.Body,
	)
	if err != nil {
		return nil, err
	}

	return &forge.InlineComment{
		ID: formatInlineCommentThreadID(comment.ID, prID),
		CommentID: &PRComment{
			ID:   comment.ID,
			PRID: prID,
		},
		Path:   req.Path,
		Lines:  req.Lines,
		Body:   req.Body,
		Author: comment.User.DisplayName,
	}, nil
}

func (r *Repository) postReply(
	ctx context.Context,
	prID int64,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	parentID, _, err := parseInlineCommentThreadID(req.InReplyTo)
	if err != nil {
		return nil, fmt.Errorf("parse inline comment ID: %w", err)
	}

	comment, err := r.replyToComment(
		ctx, prID, parentID, req.Body,
	)
	if err != nil {
		return nil, err
	}

	return &forge.InlineComment{
		ID: req.InReplyTo,
		CommentID: &PRComment{
			ID:   comment.ID,
			PRID: prID,
		},
		Body:   req.Body,
		Author: comment.User.DisplayName,
	}, nil
}

func (r *Repository) replyToComment(
	ctx context.Context,
	prID, parentID int64,
	body string,
) (*bitbucket.Comment, error) {
	comment, _, err := r.client.CommentCreate(
		ctx, r.workspace, r.repo, prID,
		&bitbucket.CommentCreateRequest{
			Content: bitbucket.Content{Raw: body},
			Parent:  &bitbucket.CommentRef{ID: parentID},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"reply to comment %d: %w",
			parentID, err,
		)
	}
	return comment, nil
}

func (r *Repository) postInlineCommentAPI(
	ctx context.Context,
	prID int64,
	filePath string,
	lines forge.InlineCommentRange,
	body string,
) (*bitbucket.Comment, error) {
	to := lines.StartLine
	comment, _, err := r.client.CommentCreate(
		ctx, r.workspace, r.repo, prID,
		&bitbucket.CommentCreateRequest{
			Content: bitbucket.Content{Raw: body},
			Inline: &bitbucket.Inline{
				Path: filePath,
				To:   &to,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create inline comment on %s:%d: %w",
			filePath, lines.StartLine, err,
		)
	}
	return comment, nil
}

// ResolveThread marks an inline comment as resolved.
func (r *Repository) ResolveThread(
	ctx context.Context,
	id forge.InlineCommentThreadID,
) error {
	commentID, prID, err := parseInlineCommentThreadID(id)
	if err != nil {
		return err
	}

	if _, _, err := r.client.CommentResolve(
		ctx, r.workspace, r.repo, prID, commentID,
		&bitbucket.CommentResolveRequest{
			Resolution: &bitbucket.Resolution{Type: "resolved"},
		},
	); err != nil {
		return fmt.Errorf("resolve thread: %w", err)
	}

	r.log.Debug("Resolved thread", "id", id)
	return nil
}

// UnresolveThread marks an inline comment as unresolved.
func (r *Repository) UnresolveThread(
	ctx context.Context,
	id forge.InlineCommentThreadID,
) error {
	commentID, prID, err := parseInlineCommentThreadID(id)
	if err != nil {
		return err
	}

	if _, _, err := r.client.CommentResolve(
		ctx, r.workspace, r.repo, prID, commentID,
		&bitbucket.CommentResolveRequest{
			Resolution: nil,
		},
	); err != nil {
		return fmt.Errorf("unresolve thread: %w", err)
	}

	r.log.Debug("Unresolved thread", "id", id)
	return nil
}

// EditComment updates the body of an existing comment.
func (r *Repository) EditComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	comment := mustPRComment(id)
	return r.updateComment(ctx, comment.PRID, comment.ID, body)
}

// Thread ID encoding for Bitbucket:
// "commentID:prID"

func formatInlineCommentThreadID(commentID, prID int64) forge.InlineCommentThreadID {
	return forge.InlineCommentThreadID(strconv.FormatInt(commentID, 10) +
		":" + strconv.FormatInt(prID, 10))
}

func parseInlineCommentThreadID(id forge.InlineCommentThreadID) (
	commentID, prID int64, err error,
) {
	threadID := string(id)
	for i := len(threadID) - 1; i >= 0; i-- {
		if threadID[i] == ':' {
			commentID, err = strconv.ParseInt(
				threadID[:i], 10, 64,
			)
			if err != nil {
				return 0, 0, fmt.Errorf(
					"parse thread ID %q: %w",
					threadID, err,
				)
			}
			prID, err = strconv.ParseInt(
				threadID[i+1:], 10, 64,
			)
			if err != nil {
				return 0, 0, fmt.Errorf(
					"parse thread ID %q: %w",
					threadID, err,
				)
			}
			return commentID, prID, nil
		}
	}
	return 0, 0, fmt.Errorf(
		"invalid thread ID format: %q", threadID,
	)
}
