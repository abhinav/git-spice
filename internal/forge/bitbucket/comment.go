package bitbucket

import (
	"context"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
)

// PostChangeComment posts a comment on a pull request.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	body string,
) (forge.ChangeCommentID, error) {
	comment, err := r.gw.CreateComment(ctx, mustPR(id).Number, body)
	if err != nil {
		return nil, err
	}
	return &PRComment{
		ID:      comment.ID,
		PRID:    comment.PRID,
		Version: comment.Version,
	}, nil
}

// UpdateChangeComment updates an existing comment on a pull request.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	comment, err := changeCommentID(id)
	if err != nil {
		return err
	}
	return r.gw.UpdateComment(ctx, comment, body)
}

// DeleteChangeComment deletes a comment on a pull request.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	comment, err := changeCommentID(id)
	if err != nil {
		return err
	}
	return r.gw.DeleteComment(ctx, comment)
}

// changeCommentID converts stored comment metadata to the gateway shape.
func changeCommentID(id forge.ChangeCommentID) (*gw.ChangeComment, error) {
	comment := mustPRComment(id)
	if comment.PRID == 0 {
		return nil, fmt.Errorf("comment %d missing PR ID: %w",
			comment.ID, forge.ErrCommentCannotUpdate)
	}
	return &gw.ChangeComment{
		ID:      comment.ID,
		PRID:    comment.PRID,
		Version: comment.Version,
	}, nil
}

// ListChangeComments lists comments on a pull request.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	opts *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	prID := mustPR(id).Number

	var listOpts gw.ListCommentsOptions
	if opts != nil {
		listOpts.CanUpdateOnly = opts.CanUpdate
	}

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		for c, err := range r.gw.ListComments(ctx, prID, listOpts) {
			if err != nil {
				yield(nil, err)
				return
			}

			if !matchesBodyFilter(c.Body, opts) {
				continue
			}

			item := &forge.ListChangeCommentItem{
				ID: &PRComment{
					ID:      c.ID,
					PRID:    c.PRID,
					Version: c.Version,
				},
				Body: c.Body,
			}
			if !yield(item, nil) {
				return
			}
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
