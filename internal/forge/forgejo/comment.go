package forgejo

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
)

// PRComment uniquely identifies a comment on a Forgejo pull request.
type PRComment struct {
	// ID is the comment ID.
	ID int64 `json:"id"` // required

	// PRNumber is the pull request number this comment belongs to.
	PRNumber int64 `json:"pr_number"` // required
}

var _ forge.ChangeCommentID = (*PRComment)(nil)

func mustPRComment(id forge.ChangeCommentID) *PRComment {
	if id == nil {
		return nil
	}
	comment, ok := id.(*PRComment)
	if !ok {
		panic(fmt.Sprintf("forgejo: expected *PRComment, got %T", id))
	}
	return comment
}

func (c *PRComment) String() string {
	return strconv.FormatInt(c.ID, 10)
}

// CommentCountsByChange returns empty counts because Forgejo does not expose
// review-comment resolution state through the modeled API surface.
func (r *Repository) CommentCountsByChange(
	_ context.Context,
	ids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	results := make([]*forge.CommentCounts, len(ids))
	for i := range ids {
		results[i] = new(forge.CommentCounts)
	}
	return results, nil
}

// PostChangeComment posts a new comment on a pull request.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	markdown string,
) (forge.ChangeCommentID, error) {
	prNumber := mustPR(id).Number
	comment, _, err := r.client.IssueCommentCreate(
		ctx,
		r.owner,
		r.repo,
		prNumber,
		&forgejo.CreateIssueCommentOption{Body: markdown},
	)
	if err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}

	return &PRComment{ID: comment.ID, PRNumber: prNumber}, nil
}

// UpdateChangeComment updates the contents of an existing pull request comment.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	markdown string,
) error {
	comment := mustPRComment(id)
	_, _, err := r.client.IssueCommentEdit(
		ctx,
		r.owner,
		r.repo,
		comment.ID,
		&forgejo.EditIssueCommentOption{Body: markdown},
	)
	if err != nil {
		if errors.Is(err, forgejo.ErrNotFound) {
			return fmt.Errorf("update comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}
	return nil
}

// DeleteChangeComment deletes an existing pull request comment.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	if _, err := r.client.IssueCommentDelete(
		ctx,
		r.owner,
		r.repo,
		mustPRComment(id).ID,
	); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

var _listChangeCommentsPageSize = 20 // var for testing

// ListChangeComments lists comments on a pull request,
// optionally applying the given filtering options.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	options *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	var filters []func(*forgejo.Comment) bool
	if options != nil {
		for _, re := range options.BodyMatchesAll {
			filters = append(filters, func(comment *forgejo.Comment) bool {
				return re.MatchString(comment.Body)
			})
		}
		if options.CanUpdate {
			filters = append(filters, func(comment *forgejo.Comment) bool {
				return r.canPush ||
					comment.User != nil && comment.User.ID == r.userID
			})
		}
	}

	prNumber := mustPR(id).Number
	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		opts := &forgejo.ListOptions{
			Page:  1,
			Limit: int64(_listChangeCommentsPageSize),
		}
		for {
			comments, response, err := r.client.IssueCommentList(
				ctx, r.owner, r.repo, prNumber, opts,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list comments: %w", err))
				return
			}

			for _, comment := range comments {
				if !commentMatches(comment, filters) {
					continue
				}
				if !yield(&forge.ListChangeCommentItem{
					ID:   &PRComment{ID: comment.ID, PRNumber: prNumber},
					Body: comment.Body,
				}, nil) {
					return
				}
			}

			if response.NextPage == 0 {
				return
			}
			opts.Page = int64(response.NextPage)
		}
	}
}

func commentMatches(
	comment *forgejo.Comment,
	filters []func(*forgejo.Comment) bool,
) bool {
	for _, filter := range filters {
		if !filter(comment) {
			return false
		}
	}
	return true
}
