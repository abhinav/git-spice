package bitbucket

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
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
) (*apiComment, error) {
	path := fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/comments",
		r.workspace, r.repo, prID,
	)

	req := &apiCreateCommentRequest{
		Content: apiContent{Raw: body},
	}

	var resp apiComment
	if err := r.client.post(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	return &resp, nil
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
	path := fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/comments/%d",
		r.workspace, r.repo, prID, commentID,
	)

	req := &apiCreateCommentRequest{
		Content: apiContent{Raw: body},
	}

	if err := r.client.put(ctx, path, req, nil); err != nil {
		// 404 means the comment doesn't exist (deleted or wrong PR).
		// Return sentinel error so caller can recreate it.
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
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
	path := fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/comments/%d",
		r.workspace, r.repo, prID, commentID,
	)
	if err := r.client.do(ctx, "DELETE", path, nil, nil); err != nil {
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
	return r.iterateComments(ctx, prID, opts)
}

func (r *Repository) iterateComments(
	ctx context.Context,
	prID int64,
	opts *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		path := r.buildCommentsPath(prID)
		r.fetchAndYieldComments(ctx, path, prID, opts, yield)
	}
}

func (r *Repository) buildCommentsPath(prID int64) string {
	return fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/comments?pagelen=%d",
		r.workspace, r.repo, prID, _listChangeCommentsPageSize,
	)
}

func (r *Repository) fetchAndYieldComments(
	ctx context.Context,
	path string,
	prID int64,
	opts *forge.ListChangeCommentsOptions,
	yield func(*forge.ListChangeCommentItem, error) bool,
) {
	for path != "" {
		comments, nextPath, err := r.fetchCommentPage(ctx, path)
		if err != nil {
			yield(nil, err)
			return
		}

		if !yieldFilteredComments(comments, prID, opts, yield) {
			return
		}
		path = nextPath
	}
}

func (r *Repository) fetchCommentPage(
	ctx context.Context,
	path string,
) ([]apiComment, string, error) {
	var resp apiCommentList
	if err := r.client.get(ctx, path, &resp); err != nil {
		return nil, "", fmt.Errorf("list comments: %w", err)
	}
	return resp.Values, resp.Next, nil
}

func yieldFilteredComments(
	comments []apiComment,
	prID int64,
	opts *forge.ListChangeCommentsOptions,
	yield func(*forge.ListChangeCommentItem, error) bool,
) bool {
	for _, c := range comments {
		if !matchesBodyFilter(c.Content.Raw, opts) {
			continue
		}
		if !yield(convertComment(&c, prID), nil) {
			return false
		}
	}
	return true
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

func convertComment(c *apiComment, prID int64) *forge.ListChangeCommentItem {
	return &forge.ListChangeCommentItem{
		ID:   &PRComment{ID: c.ID, PRID: prID},
		Body: c.Content.Raw,
	}
}
