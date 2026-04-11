package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// _listChangeCommentsPageSize is the number of comments to fetch per page.
// It's a variable so tests can override it.
var _listChangeCommentsPageSize = 100

// PostChangeComment posts a comment on a pull request.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	body string,
) (forge.ChangeCommentID, error) {
	prID := mustPR(id).Number
	comment, err := r.createComment(ctx, prID, body)
	if err != nil {
		return nil, err
	}
	return &PRComment{ID: comment.ID, PRID: prID}, nil
}

func (r *Repository) createComment(
	ctx context.Context,
	prID int64,
	body string,
) (*bitbucket.Comment, error) {
	comment, _, err := r.client.CommentCreate(ctx, r.workspace, r.repo, prID,
		&bitbucket.CommentCreateRequest{
			Content: bitbucket.Content{Raw: body},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	return comment, nil
}

// UpdateChangeComment updates an existing comment on a pull request.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	comment := mustPRComment(id)
	return r.updateComment(ctx, comment.PRID, comment.ID, body)
}

func (r *Repository) updateComment(
	ctx context.Context,
	prID, commentID int64,
	body string,
) error {
	_, _, err := r.client.CommentUpdate(ctx, r.workspace, r.repo, prID, commentID,
		&bitbucket.CommentCreateRequest{
			Content: bitbucket.Content{Raw: body},
		},
	)
	if err != nil {
		// 404 means the comment doesn't exist (deleted or wrong PR).
		// Return sentinel error so caller can recreate it.
		if errors.Is(err, bitbucket.ErrNotFound) {
			return fmt.Errorf("comment %d not found: %w", commentID, forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}
	return nil
}

// DeleteChangeComment deletes a comment on a pull request.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	comment := mustPRComment(id)
	if comment.PRID == 0 {
		return fmt.Errorf("comment %d missing PR ID: %w",
			comment.ID, forge.ErrCommentCannotUpdate)
	}
	return r.deleteComment(ctx, comment.PRID, comment.ID)
}

func (r *Repository) deleteComment(
	ctx context.Context,
	prID, commentID int64,
) error {
	if _, err := r.client.CommentDelete(ctx, r.workspace, r.repo, prID, commentID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

// ListChangeComments lists comments on a pull request.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	opts *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	prID := mustPR(id).Number

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		for c, err := range r.listPullRequestComments(ctx, prID) {
			if err != nil {
				yield(nil, err)
				return
			}

			if !matchesBodyFilter(c.Content.Raw, opts) {
				continue
			}

			if !yield(convertComment(c, prID), nil) {
				return
			}
		}
	}
}

func (r *Repository) listPullRequestComments(
	ctx context.Context, prID int64,
) iter.Seq2[*bitbucket.Comment, error] {
	return func(yield func(*bitbucket.Comment, error) bool) {
		listOptions := bitbucket.CommentListOptions{
			PageLen: _listChangeCommentsPageSize,
		}

		for {
			comments, resp, err := r.client.CommentList(
				ctx, r.workspace, r.repo, prID, &listOptions,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list comments: %w", err))
				return
			}

			for _, c := range comments.Values {
				if !yield(&c, nil) {
					return
				}
			}

			if resp.NextURL == "" {
				return
			}
			listOptions.PageURL = resp.NextURL
		}
	}
}

func matchesBodyFilter(body string, opts *forge.ListChangeCommentsOptions) bool {
	if opts == nil || opts.BodyMatchesAll == nil {
		return true
	}
	for _, re := range opts.BodyMatchesAll {
		if !re.MatchString(body) {
			return false
		}
	}
	return true
}

func convertComment(
	c *bitbucket.Comment,
	prID int64,
) *forge.ListChangeCommentItem {
	return &forge.ListChangeCommentItem{
		ID:   &PRComment{ID: c.ID, PRID: prID},
		Body: c.Content.Raw,
	}
}
