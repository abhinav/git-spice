package gitea

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// UserCurrent fetches the authenticated Gitea user.
//
// Gitea API:
// https://gitea.com/api/swagger#/user/userGetCurrent
func (c *Client) UserCurrent(ctx context.Context) (*User, *Response, error) {
	var response User
	resp, err := c.get(ctx, "user", nil, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullCreate creates a pull request.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoCreatePullRequest
func (c *Client) PullCreate(
	ctx context.Context,
	owner, repo string,
	opt *CreatePullRequestOption,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.post(
		ctx,
		fmt.Sprintf("repos/%s/%s/pulls", owner, repo),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullGet fetches a single pull request by index.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoGetPullRequest
func (c *Client) PullGet(
	ctx context.Context,
	owner, repo string,
	index int64,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, index),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullEdit updates a pull request.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoEditPullRequest
func (c *Client) PullEdit(
	ctx context.Context,
	owner, repo string,
	index int64,
	opt *EditPullRequestOption,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.patch(
		ctx,
		fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, index),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullList lists pull requests for a repository.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoListPullRequests
func (c *Client) PullList(
	ctx context.Context,
	owner, repo string,
	opt *ListPullRequestsOptions,
) ([]*PullRequest, *Response, error) {
	var response []*PullRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s/pulls", owner, repo),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// PullMerge merges a pull request.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoMergePullRequest
func (c *Client) PullMerge(
	ctx context.Context,
	owner, repo string,
	index int64,
	opt *MergePullRequestOption,
) (*Response, error) {
	return c.post(
		ctx,
		fmt.Sprintf("repos/%s/%s/pulls/%d/merge", owner, repo, index),
		nil,
		opt,
		nil,
	)
}

// ReviewRequestCreate adds reviewer requests to a pull request.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoCreatePullReviewRequests
func (c *Client) ReviewRequestCreate(
	ctx context.Context,
	owner, repo string,
	index int64,
	reviewers []string,
) (*Response, error) {
	return c.post(
		ctx,
		fmt.Sprintf("repos/%s/%s/pulls/%d/requested_reviewers", owner, repo, index),
		nil,
		&reviewRequestOption{Reviewers: reviewers},
		nil,
	)
}

type reviewRequestOption struct {
	Reviewers []string `json:"reviewers"`
}

// CommentCreate creates a comment on an issue or pull request.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueCreateComment
func (c *Client) CommentCreate(
	ctx context.Context,
	owner, repo string,
	index int64,
	body string,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.post(
		ctx,
		fmt.Sprintf("repos/%s/%s/issues/%d/comments", owner, repo, index),
		nil,
		&createCommentOption{Body: body},
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentEdit updates an existing comment.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueEditComment
func (c *Client) CommentEdit(
	ctx context.Context,
	owner, repo string,
	id int64,
	body string,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.patch(
		ctx,
		fmt.Sprintf("repos/%s/%s/issues/comments/%d", owner, repo, id),
		nil,
		&editCommentOption{Body: body},
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentDelete deletes a comment.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueDeleteComment
func (c *Client) CommentDelete(
	ctx context.Context,
	owner, repo string,
	id int64,
) (*Response, error) {
	return c.delete(
		ctx,
		fmt.Sprintf("repos/%s/%s/issues/comments/%d", owner, repo, id),
		nil,
	)
}

// CommentList lists comments on an issue or pull request.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueGetComments
func (c *Client) CommentList(
	ctx context.Context,
	owner, repo string,
	index int64,
	opt *ListIssueCommentsOptions,
) ([]*Comment, *Response, error) {
	var response []*Comment
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s/issues/%d/comments", owner, repo, index),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// CommitStatusCombined fetches the combined commit status for a SHA.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoGetCombinedStatusByRef
func (c *Client) CommitStatusCombined(
	ctx context.Context,
	owner, repo, sha string,
) (*CombinedStatus, *Response, error) {
	var response CombinedStatus
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s/commits/%s/status", owner, repo, sha),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// RepoGet fetches a repository by owner and name.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoGet
func (c *Client) RepoGet(
	ctx context.Context,
	owner, repo string,
) (*Repository, *Response, error) {
	var response Repository
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s", owner, repo),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// Repository matches the subset of repository fields the forge uses.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoGet
type Repository struct {
	ID          int64                 `json:"id"`
	FullName    string                `json:"full_name"`
	Permissions *RepositoryPermission `json:"permissions,omitempty"`
}

// RepositoryPermission holds the current user's access level for a repository.
type RepositoryPermission struct {
	Admin bool `json:"admin"`
	Push  bool `json:"push"`
	Pull  bool `json:"pull"`
}

// CommitStatusCreate creates a commit status for a SHA.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoCreateStatus
func (c *Client) CommitStatusCreate(
	ctx context.Context,
	owner, repo, sha string,
	opt *CreateCommitStatusOption,
) (*CommitStatus, *Response, error) {
	var response CommitStatus
	resp, err := c.post(
		ctx,
		fmt.Sprintf("repos/%s/%s/statuses/%s", owner, repo, sha),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommitStatus is the status of a single commit context.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoListStatuses
type CommitStatus struct {
	ID      int64  `json:"id"`
	State   string `json:"status"`
	Context string `json:"context"`
}

// CreateCommitStatusOption configures commit status creation.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoCreateStatus
type CreateCommitStatusOption struct {
	// State is the commit status state:
	// "pending", "success", "failure", "error", or "warning".
	State string `json:"state"`

	// Context identifies the status source (e.g., "ci/tests").
	Context string `json:"context,omitempty"`

	// Description is a short description of the status.
	Description string `json:"description,omitempty"`
}

// LabelList lists labels for a repository.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueListLabels
func (c *Client) LabelList(
	ctx context.Context,
	owner, repo string,
	opt *ListLabelsOptions,
) ([]*Label, *Response, error) {
	var response []*Label
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s/labels", owner, repo),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// FileContent fetches the contents of a file in a repository.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoGetContents
func (c *Client) FileContent(
	ctx context.Context,
	owner, repo, path string,
) (*FileContentResponse, *Response, error) {
	var response FileContentResponse
	resp, err := c.get(
		ctx,
		fmt.Sprintf("repos/%s/%s/contents/%s", owner, repo, path),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// User matches the subset of user fields the forge uses.
//
// Gitea API:
// https://gitea.com/api/swagger#/user/userGetCurrent
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// PRBranch holds branch information embedded in a pull request.
type PRBranch struct {
	// Label is "owner:branch" for fork PRs, "branch" for same-repo PRs.
	Label string `json:"label"`
	Ref   string `json:"ref"`
	Sha   string `json:"sha"`
}

// PullRequest matches the subset of pull request fields the forge uses.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoGetPullRequest
type PullRequest struct {
	Number             int64     `json:"number"`
	Title              string    `json:"title"`
	Body               string    `json:"body"`
	State              string    `json:"state"` // "open", "closed"
	Merged             bool      `json:"merged"`
	Draft              bool      `json:"draft"`
	Head               *PRBranch `json:"head"`
	Base               *PRBranch `json:"base"`
	Labels             []*Label  `json:"labels"`
	Assignees          []*User   `json:"assignees"`
	RequestedReviewers []*User   `json:"requested_reviewers"`
	HTMLURL            string    `json:"html_url"`
}

// Label matches the subset of label fields the forge uses.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueListLabels
type Label struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CombinedStatus is the combined commit status for a ref.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoGetCombinedStatusByRef
type CombinedStatus struct {
	// State is the aggregate state:
	// "pending", "success", "failure", "error", "warning", or "".
	State string `json:"state"`
}

// Gitea combined commit status state values.
const (
	CommitStatusPending = "pending"
	CommitStatusSuccess = "success"
	CommitStatusFailure = "failure"
	CommitStatusError   = "error"
	CommitStatusWarning = "warning"
)

// Comment matches the subset of comment fields the forge uses.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueGetComment
type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User *User  `json:"user"`
}

// FileContentResponse matches the subset of file content fields the forge uses.
//
// Gitea API:
// https://gitea.com/api/swagger#/repository/repoGetContents
type FileContentResponse struct {
	// Content is the base64-encoded file content.
	Content string `json:"content"`
	// Encoding is the encoding of Content (always "base64").
	Encoding string `json:"encoding"`
	// Name is the file name.
	Name string `json:"name"`
}

// ListOptions configures offset pagination.
type ListOptions struct {
	Page  int64
	Limit int64
}

// CreatePullRequestOption configures pull request creation.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoCreatePullRequest
type CreatePullRequestOption struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Head      string   `json:"head"`
	Base      string   `json:"base"`
	Draft     bool     `json:"draft,omitempty"`
	Labels    []int64  `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
}

// EditPullRequestOption configures pull request updates.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoEditPullRequest
type EditPullRequestOption struct {
	Title     *string  `json:"title,omitempty"`
	Body      *string  `json:"body,omitempty"`
	Base      *string  `json:"base,omitempty"`
	State     *string  `json:"state,omitempty"` // "open" or "closed"
	Draft     *bool    `json:"draft,omitempty"`
	Labels    []int64  `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
}

// MergePullRequestOption configures pull request merging.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoMergePullRequest
type MergePullRequestOption struct {
	// Do is the merge strategy: "merge", "squash", or "rebase".
	Do string `json:"Do"`

	// HeadCommitID, if non-empty, aborts the merge
	// if the pull request head no longer matches.
	HeadCommitID string `json:"head_commit_id,omitempty"`
}

// ListPullRequestsOptions configures pull request listing.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/repoListPullRequests
type ListPullRequestsOptions struct {
	ListOptions

	// State filters by PR state: "open", "closed", or "".
	State string

	// Head filters by head branch name.
	Head string

	// Limit is the maximum number of results to return.
	Limit int64
}

func (o *ListPullRequestsOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.State != "" {
		values.Set("state", o.State)
	}
	if o.Head != "" {
		values.Set("head", o.Head)
	}
	if o.Limit != 0 {
		values.Set("limit", strconv.FormatInt(o.Limit, 10))
	}
	if o.Page != 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	return values
}

// ListIssueCommentsOptions configures issue comment listing.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueGetComments
type ListIssueCommentsOptions struct {
	ListOptions
}

func (o *ListIssueCommentsOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.Limit != 0 {
		values.Set("limit", strconv.FormatInt(o.Limit, 10))
	}
	if o.Page != 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	return values
}

// ListLabelsOptions configures label listing.
//
// Gitea API:
// https://gitea.com/api/swagger#/issue/issueListLabels
type ListLabelsOptions struct {
	ListOptions
}

func (o *ListLabelsOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.Limit != 0 {
		values.Set("limit", strconv.FormatInt(o.Limit, 10))
	}
	if o.Page != 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	return values
}

type createCommentOption struct {
	Body string `json:"body"`
}

type editCommentOption struct {
	Body string `json:"body"`
}
