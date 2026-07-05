package forgejo

import (
	"context"
	"fmt"
)

// PullReviewList lists pull request reviews.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoListPullReviews
func (c *Client) PullReviewList(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *ListOptions,
) ([]*PullReview, *Response, error) {
	var response []*PullReview
	resp, err := c.get(
		ctx,
		fmt.Sprintf("%s/pulls/%d/reviews", repoPath(owner, repo), index),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// PullReviewCreate creates a pull request review.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoCreatePullReview
func (c *Client) PullReviewCreate(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *CreatePullReviewOptions,
) (*PullReview, *Response, error) {
	var response PullReview
	resp, err := c.post(
		ctx,
		fmt.Sprintf("%s/pulls/%d/reviews", repoPath(owner, repo), index),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullReviewCommentList lists comments for a pull request review.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetPullReviewComments
func (c *Client) PullReviewCommentList(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	reviewID int64,
	opt *ListOptions,
) ([]*PullReviewComment, *Response, error) {
	var response []*PullReviewComment
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"%s/pulls/%d/reviews/%d/comments",
			repoPath(owner, repo),
			index,
			reviewID,
		),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// PullReviewCommentCreate creates a pull request review comment.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoCreatePullReviewComment
func (c *Client) PullReviewCommentCreate(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	reviewID int64,
	opt *CreatePullReviewCommentOptions,
) (*Response, error) {
	return c.post(
		ctx,
		fmt.Sprintf(
			"%s/pulls/%d/reviews/%d/comments",
			repoPath(owner, repo),
			index,
			reviewID,
		),
		nil,
		opt,
		nil,
	)
}

// ReviewerList lists users who can review pull requests.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetReviewers
func (c *Client) ReviewerList(
	ctx context.Context,
	owner string,
	repo string,
	opt *ListOptions,
) ([]*User, *Response, error) {
	var response []*User
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/reviewers",
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// PullReviewRequestCreate requests pull request reviews.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoCreatePullReviewRequests
func (c *Client) PullReviewRequestCreate(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *PullReviewRequestOptions,
) (*Response, error) {
	var response []*User
	resp, err := c.post(
		ctx,
		fmt.Sprintf(
			"%s/pulls/%d/requested_reviewers",
			repoPath(owner, repo),
			index,
		),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return resp, err
	}
	return resp, nil
}

// PullReviewRequestDelete removes pull request review requests.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoDeletePullReviewRequests
func (c *Client) PullReviewRequestDelete(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *PullReviewRequestOptions,
) (*Response, error) {
	return c.deleteWithBody(
		ctx,
		fmt.Sprintf(
			"%s/pulls/%d/requested_reviewers",
			repoPath(owner, repo),
			index,
		),
		nil,
		opt,
	)
}

// PullReview matches a Forgejo pull request review.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/PullReview
type PullReview struct {
	// ID is the review ID.
	ID int64 `json:"id"`

	// State is the review state.
	State string `json:"state"`

	// Body is the review body.
	Body string `json:"body"`

	// User is the review author.
	User *User `json:"user"`
}

// PullReviewComment matches a Forgejo pull request review comment.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/PullReviewComment
type PullReviewComment struct {
	// ID is the review comment ID.
	ID int64 `json:"id"`

	// Body is the review comment body.
	Body string `json:"body"`

	// Path is the commented file path.
	Path string `json:"path"`

	// DiffHunk is the diff hunk attached to the comment.
	DiffHunk string `json:"diff_hunk"`

	// Position is the diff position.
	Position int64 `json:"position"`

	// OriginalPosition is the original diff position.
	OriginalPosition int64 `json:"original_position"`

	// CommitID is the commit SHA for the comment.
	CommitID string `json:"commit_id"`

	// OriginalCommitID is the original commit SHA for the comment.
	OriginalCommitID string `json:"original_commit_id"`

	// User is the comment author.
	User *User `json:"user"`
}

// CreatePullReviewOptions is the request body for creating a review.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CreatePullReviewOptions
type CreatePullReviewOptions struct {
	// Body is the review body.
	Body string `json:"body,omitempty"`

	// Event selects the review action.
	Event string `json:"event,omitempty"`

	// Comments are draft review comments to create with the review.
	Comments []CreatePullReviewCommentOptions `json:"comments,omitempty"`
}

// CreatePullReviewCommentOptions is the request body for a review comment.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CreatePullReviewCommentOptions
type CreatePullReviewCommentOptions struct {
	// Body is the comment body.
	Body string `json:"body"`

	// Path is the commented file path.
	Path string `json:"path"`

	// NewPosition is the position in the new diff.
	NewPosition int64 `json:"new_position,omitempty"`

	// OldPosition is the position in the old diff.
	OldPosition int64 `json:"old_position,omitempty"`

	// CommitID is the commit SHA for the comment.
	CommitID string `json:"commit_id,omitempty"`
}

// PullReviewRequestOptions is the request body for review requests.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/PullReviewRequestOptions
type PullReviewRequestOptions struct {
	// Reviewers are reviewer login names.
	Reviewers []string `json:"reviewers,omitempty"`

	// TeamReviewers are team names requested for review.
	TeamReviewers []string `json:"team_reviewers,omitempty"`
}
