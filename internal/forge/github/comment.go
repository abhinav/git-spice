package github

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// PRComment is a ChangeCommentID for a GitHub PR comment.
type PRComment struct {
	// GQLID is the comment's GraphQL node ID.
	GQLID github.ID `json:"gqlID,omitempty"`

	// URL is GitHub's browser URL for the comment.
	URL string `json:"url,omitempty"`
}

var _ forge.ChangeCommentID = (*PRComment)(nil)

func mustPRComment(id forge.ChangeCommentID) *PRComment {
	if id == nil {
		return nil
	}

	prc, ok := id.(*PRComment)
	if !ok {
		panic(fmt.Sprintf("unexpected PR comment type: %T", id))
	}
	return prc
}

func (c *PRComment) String() string {
	return c.URL
}

// PostChangeComment posts a new comment on a PR.
func (r *Repository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	markdown string,
) (forge.ChangeCommentID, error) {
	pr := mustPR(id)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return nil, err
	}

	comment, err := r.gateway.AddComment(ctx, gqlID, markdown)
	if err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}

	r.log.Debug("Posted comment", "url", comment.URL)
	return &PRComment{
		GQLID: comment.ID,
		URL:   comment.URL,
	}, nil
}

// UpdateChangeComment updates the contents of an existing comment on a PR.
func (r *Repository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	markdown string,
) error {
	cid := mustPRComment(id)
	gqlID := cid.GQLID

	if err := r.gateway.UpdateIssueComment(ctx, gqlID, markdown); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return fmt.Errorf("update comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update comment: %w", err)
	}

	r.log.Debug("Updated comment", "url", cid.URL)
	return nil
}

// DeleteChangeComment deletes an existing comment on a PR.
func (r *Repository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	// DeleteChangeComment isn't part of the forge.Repository interface.
	// It's just nice to have to clean up after the integration test.
	cid := mustPRComment(id)
	gqlID := cid.GQLID

	if err := r.gateway.DeleteIssueComment(ctx, gqlID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	r.log.Debug("Deleted comment", "url", cid.URL)

	return nil
}

// There isn't a way to filter comments by contents server-side,
// so we'll be doing that client-side.
// GitHub's GraphQL API rate limits based on the number of nodes queried,
// so we'll be fetching comments in pages of 10 instead of an obnoxious number.
//
// Since our comment will usually be among the first few comments,
// that, plus the ascending order of comments, should make this good enough.
var _listChangeCommentsPageSize = 10 // var for testing

// ListChangeComments lists comments on a PR,
// optionally applying the given filtering options.
func (r *Repository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	options *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	var filters []func(*github.Comment) (keep bool)
	if options != nil {
		if len(options.BodyMatchesAll) != 0 {
			for _, re := range options.BodyMatchesAll {
				filters = append(filters, func(node *github.Comment) bool {
					return re.MatchString(node.Body)
				})
			}
		}
		if options.CanUpdate {
			filters = append(filters, func(node *github.Comment) bool {
				return node.ViewerCanUpdate
			})
		}
	}

	gqlID, err := r.graphQLID(ctx, mustPR(id))
	if err != nil {
		return func(yield func(*forge.ListChangeCommentItem, error) bool) {
			yield(nil, err)
		}
	}

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		for node, err := range r.gateway.PullRequestComments(ctx, gqlID, &github.PaginationOptions{
			ItemsPerPage: _listChangeCommentsPageSize,
		}) {
			if err != nil {
				yield(nil, err)
				return
			}

			if slices.ContainsFunc(filters, func(keep func(*github.Comment) bool) bool {
				return !keep(node)
			}) {
				continue
			}

			if !yield(&forge.ListChangeCommentItem{
				ID: &PRComment{
					GQLID: node.ID,
					URL:   node.URL,
				},
				Body: node.Body,
			}, nil) {
				return
			}
		}
	}
}
