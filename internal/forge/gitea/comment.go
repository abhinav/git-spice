package gitea

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

// PostChangeComment posts a new comment on a pull request.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	markdown string,
) (forge.ChangeCommentID, error) {
	prNumber := mustPR(id).Number
	comment, _, err := r.client.CommentCreate(ctx, r.owner, r.repo, prNumber, markdown)
	if err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}

	r.log.Debug("Posted comment", "id", comment.ID, "pr", prNumber)
	return &PRComment{ID: comment.ID}, nil
}

// UpdateChangeComment updates the contents of an existing comment on a PR.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	markdown string,
) error {
	c := mustPRComment(id)
	_, _, err := r.client.CommentEdit(ctx, r.owner, r.repo, c.ID, markdown)
	if err != nil {
		if errors.Is(err, giteagw.ErrNotFound) {
			return fmt.Errorf("update comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}
	r.log.Debug("Updated comment", "id", c.ID)
	return nil
}

// DeleteChangeComment deletes an existing comment on a PR.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	c := mustPRComment(id)
	_, err := r.client.CommentDelete(ctx, r.owner, r.repo, c.ID)
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	r.log.Debug("Deleted comment", "id", c.ID)
	return nil
}

// _listChangeCommentsPageSize controls how many comments are fetched per page.
// var for testing.
var _listChangeCommentsPageSize = 20

// ListChangeComments lists comments on a pull request,
// optionally applying the given filtering options.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	options *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	var filters []func(*giteagw.Comment) bool

	if options != nil {
		for _, re := range options.BodyMatchesAll {
			filters = append(filters, func(c *giteagw.Comment) bool {
				return re.MatchString(c.Body)
			})
		}

		if options.CanUpdate {
			filters = append(filters, func(c *giteagw.Comment) bool {
				return c.User != nil && c.User.ID == r.userID
			})
		}
	}

	prNumber := mustPR(id).Number

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		opts := &giteagw.ListIssueCommentsOptions{
			ListOptions: giteagw.ListOptions{
				Limit: int64(_listChangeCommentsPageSize),
			},
		}

		for pageNum := 1; true; pageNum++ {
			comments, resp, err := r.client.CommentList(
				ctx, r.owner, r.repo, prNumber, opts,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list comments (page %d): %w", pageNum, err))
				return
			}

			for _, comment := range comments {
				match := true
				for _, filter := range filters {
					if !filter(comment) {
						match = false
						break
					}
				}
				if !match {
					continue
				}

				item := &forge.ListChangeCommentItem{
					ID:   &PRComment{ID: comment.ID},
					Body: comment.Body,
				}
				if !yield(item, nil) {
					return
				}
			}

			if resp.NextPage == 0 {
				return
			}
			opts.Page = int64(resp.NextPage)
		}
	}
}

// SetListChangeCommentsPageSize overrides the page size for testing.
func SetListChangeCommentsPageSize(n int) {
	_listChangeCommentsPageSize = n
}
