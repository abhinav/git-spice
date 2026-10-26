package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
)

// PostChangeComment creates a new comment thread on a pull request.
// Returns the ID of the first comment in the thread.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	body string,
) (forge.ChangeCommentID, error) {
	prID := mustPR(id).Number

	threadID, commentID, err := r.gateway.AddComment(
		ctx, r.project(), r.repositoryID(), prID, body,
	)
	if err != nil {
		return nil, fmt.Errorf("create comment thread: %w", err)
	}

	return &PRComment{
		PRID:      prID,
		ThreadID:  threadID,
		CommentID: commentID,
	}, nil
}

// UpdateChangeComment updates an existing comment in a thread.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	comment := mustPRComment(id)

	exists, err := r.gateway.CommentExists(
		ctx, r.project(), r.repositoryID(),
		comment.PRID, comment.ThreadID, comment.CommentID,
	)
	if err != nil {
		if errors.Is(err, azuredevops.ErrNotFound) {
			return fmt.Errorf("get comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("get comment: %w", err)
	}
	if !exists {
		return fmt.Errorf("get comment: %w", forge.ErrNotFound)
	}

	err = r.gateway.UpdateComment(
		ctx, r.project(), r.repositoryID(),
		comment.PRID, comment.ThreadID, comment.CommentID, body,
	)
	if err != nil {
		if errors.Is(err, azuredevops.ErrNotFound) {
			return fmt.Errorf("update comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}

	r.log.Debug("Updated comment",
		"pr", comment.PRID,
		"thread", comment.ThreadID,
		"comment", comment.CommentID)
	return nil
}

// DeleteChangeComment deletes an existing comment from a pull request thread.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	comment := mustPRComment(id)

	if err := r.gateway.DeleteComment(
		ctx, r.project(), r.repositoryID(),
		comment.PRID, comment.ThreadID, comment.CommentID,
	); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	r.log.Debug("Deleted comment",
		"pr", comment.PRID,
		"thread", comment.ThreadID,
		"comment", comment.CommentID)
	return nil
}

// ListChangeComments lists comments on a pull request.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	opts *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		prID := mustPR(id).Number
		var currentUserID string
		if opts != nil && opts.CanUpdate {
			var err error
			currentUserID, err = r.gateway.CurrentUserID(ctx)
			if err != nil {
				yield(nil, fmt.Errorf("get current user: %w", err))
				return
			}
		}

		threads, err := r.gateway.Threads(
			ctx, r.project(), r.repositoryID(), prID,
		)
		if err != nil {
			yield(nil, fmt.Errorf("get threads: %w", err))
			return
		}

		for _, thread := range threads {
			if len(thread.Comments) == 0 || thread.Deleted {
				continue
			}

			for _, comment := range thread.Comments {
				if comment.Deleted {
					continue
				}

				body := comment.Body
				if comment.System {
					continue
				}

				// Apply filters if specified.
				if opts != nil {
					// Filter by body matches.
					if len(opts.BodyMatchesAll) > 0 {
						allMatch := true
						for _, re := range opts.BodyMatchesAll {
							if !re.MatchString(body) {
								allMatch = false
								break
							}
						}
						if !allMatch {
							continue
						}
					}

					if opts.CanUpdate &&
						comment.AuthorID != currentUserID {
						continue
					}
				}

				item := &forge.ListChangeCommentItem{
					ID: &PRComment{
						PRID:      prID,
						ThreadID:  thread.ID,
						CommentID: comment.ID,
					},
					Body: body,
				}

				if !yield(item, nil) {
					return
				}
			}
		}
	}
}
