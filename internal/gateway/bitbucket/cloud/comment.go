package cloud

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

// ListChangeCommentsPageSize is the number of comments to fetch per page.
// It's a variable, exported so that tests in other packages can override it.
var ListChangeCommentsPageSize = 100

// CreateComment posts a new comment on a pull request.
func (g *Gateway) CreateComment(
	ctx context.Context,
	prID int64,
	body string,
) (*bitbucket.ChangeComment, error) {
	comment, _, err := g.client.CommentCreate(ctx, g.workspace, g.repo, prID,
		&CommentCreateRequest{
			Content: Content{Raw: body},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}

	return &bitbucket.ChangeComment{
		ID:   comment.ID,
		PRID: prID,
		Body: comment.Content.Raw,
	}, nil
}

// UpdateComment replaces the body of an existing comment.
func (g *Gateway) UpdateComment(
	ctx context.Context,
	c *bitbucket.ChangeComment,
	body string,
) error {
	_, _, err := g.client.CommentUpdate(ctx, g.workspace, g.repo, c.PRID, c.ID,
		&CommentCreateRequest{
			Content: Content{Raw: body},
		},
	)
	if err != nil {
		// 404 means the comment doesn't exist (deleted or wrong PR).
		// Return the sentinel error so the caller can recreate it.
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("comment %d not found: %w", c.ID, forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}
	return nil
}

// DeleteComment deletes an existing comment.
func (g *Gateway) DeleteComment(
	ctx context.Context,
	c *bitbucket.ChangeComment,
) error {
	_, err := g.client.CommentDelete(ctx, g.workspace, g.repo, c.PRID, c.ID)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

// ListComments lists comments on a pull request.
//
// Bitbucket Cloud cannot filter comments by author,
// so opts.CanUpdateOnly is ignored.
func (g *Gateway) ListComments(
	ctx context.Context,
	prID int64,
	_ bitbucket.ListCommentsOptions,
) iter.Seq2[*bitbucket.ChangeComment, error] {
	return func(yield func(*bitbucket.ChangeComment, error) bool) {
		for c, err := range g.listPullRequestComments(ctx, prID) {
			if err != nil {
				yield(nil, err)
				return
			}

			ok := yield(&bitbucket.ChangeComment{
				ID:   c.ID,
				PRID: prID,
				Body: c.Content.Raw,
			}, nil)
			if !ok {
				return
			}
		}
	}
}

// ResolvableComments lists review comments on a pull request
// that participate in comment resolution counts.
//
// Only inline code review comments are resolvable on Bitbucket Cloud,
// and the product has no notion of pending comments.
func (g *Gateway) ResolvableComments(
	ctx context.Context,
	prID int64,
) iter.Seq2[*bitbucket.ResolvableComment, error] {
	return func(yield func(*bitbucket.ResolvableComment, error) bool) {
		for c, err := range g.listPullRequestComments(ctx, prID) {
			if err != nil {
				yield(nil, err)
				return
			}

			// Only inline code review comments are resolvable.
			if c.Inline == nil {
				continue
			}

			ok := yield(&bitbucket.ResolvableComment{
				ID:       c.ID,
				Body:     c.Content.Raw,
				Resolved: c.Resolution != nil,
			}, nil)
			if !ok {
				return
			}
		}
	}
}

// listPullRequestComments iterates over all comments
// on the given pull request, following URL-based pagination.
func (g *Gateway) listPullRequestComments(
	ctx context.Context,
	prID int64,
) iter.Seq2[*Comment, error] {
	return func(yield func(*Comment, error) bool) {
		listOptions := CommentListOptions{
			PageLen: ListChangeCommentsPageSize,
		}

		for {
			comments, resp, err := g.client.CommentList(
				ctx, g.workspace, g.repo, prID, &listOptions,
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
