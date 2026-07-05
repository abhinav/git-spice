package forgejo

import (
	"context"
	"fmt"
)

// IssueCommentList lists issue comments for a pull request issue.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/issue/issueGetComments
func (c *Client) IssueCommentList(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *ListOptions,
) ([]*Comment, *Response, error) {
	var response []*Comment
	resp, err := c.get(
		ctx,
		fmt.Sprintf("%s/issues/%d/comments", repoPath(owner, repo), index),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// IssueCommentCreate creates an issue comment.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/issue/issueCreateComment
func (c *Client) IssueCommentCreate(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *CreateIssueCommentOption,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.post(
		ctx,
		fmt.Sprintf("%s/issues/%d/comments", repoPath(owner, repo), index),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// IssueCommentEdit updates an issue comment.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/issue/issueEditComment
func (c *Client) IssueCommentEdit(
	ctx context.Context,
	owner string,
	repo string,
	id int64,
	opt *EditIssueCommentOption,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.patch(
		ctx,
		fmt.Sprintf("%s/issues/comments/%d", repoPath(owner, repo), id),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// IssueCommentDelete deletes an issue comment.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/issue/issueDeleteComment
func (c *Client) IssueCommentDelete(
	ctx context.Context,
	owner string,
	repo string,
	id int64,
) (*Response, error) {
	return c.delete(
		ctx,
		fmt.Sprintf("%s/issues/comments/%d", repoPath(owner, repo), id),
		nil,
	)
}

// Comment matches a Forgejo issue comment.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/Comment
type Comment struct {
	// ID is the comment ID.
	ID int64 `json:"id"`

	// HTMLURL is the web URL for the comment.
	HTMLURL string `json:"html_url"`

	// PullRequestURL links to the pull request when present.
	PullRequestURL string `json:"pull_request_url"`

	// IssueURL links to the issue.
	IssueURL string `json:"issue_url"`

	// Body is the comment body.
	Body string `json:"body"`

	// User is the comment author.
	User *User `json:"user"`
}

// CreateIssueCommentOption is the request body for creating a comment.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CreateIssueCommentOption
type CreateIssueCommentOption struct {
	// Body is the comment body.
	Body string `json:"body"`
}

// EditIssueCommentOption is the request body for editing a comment.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/EditIssueCommentOption
type EditIssueCommentOption struct {
	// Body is the updated comment body.
	Body string `json:"body"`
}
