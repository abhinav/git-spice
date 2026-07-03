package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Forgejo API references:
// https://codeberg.org/api/swagger
// https://codeberg.org/swagger.v1.json

// UserCurrent fetches the authenticated Forgejo user.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/user/userGetCurrent
func (c *Client) UserCurrent(ctx context.Context) (*User, *Response, error) {
	var response User
	resp, err := c.get(ctx, "user", nil, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// RepositoryGet fetches a repository.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGet
func (c *Client) RepositoryGet(
	ctx context.Context,
	owner string,
	repo string,
) (*Repository, *Response, error) {
	var response Repository
	resp, err := c.get(ctx, repoPath(owner, repo), nil, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestList lists pull requests for a repository.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoListPullRequests
func (c *Client) PullRequestList(
	ctx context.Context,
	owner string,
	repo string,
	opt *PullRequestListOptions,
) ([]*PullRequest, *Response, error) {
	var response []*PullRequest
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/pulls",
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// PullRequestCreate creates a pull request.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoCreatePullRequest
func (c *Client) PullRequestCreate(
	ctx context.Context,
	owner string,
	repo string,
	opt *CreatePullRequestOption,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.post(
		ctx,
		repoPath(owner, repo)+"/pulls",
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestGet fetches a pull request by index.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetPullRequest
func (c *Client) PullRequestGet(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf("%s/pulls/%d", repoPath(owner, repo), index),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestEdit updates a pull request.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoEditPullRequest
func (c *Client) PullRequestEdit(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *EditPullRequestOption,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.patch(
		ctx,
		fmt.Sprintf("%s/pulls/%d", repoPath(owner, repo), index),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestGetByBaseHead fetches a pull request by base and head branch.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetPullRequestByBaseHead
func (c *Client) PullRequestGetByBaseHead(
	ctx context.Context,
	owner string,
	repo string,
	base string,
	head string,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"%s/pulls/%s/%s",
			repoPath(owner, repo),
			url.PathEscape(base),
			url.PathEscape(head),
		),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestIsMerged checks whether a pull request has been merged.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoPullRequestIsMerged
func (c *Client) PullRequestIsMerged(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
) (*Response, error) {
	return c.get(
		ctx,
		fmt.Sprintf("%s/pulls/%d/merge", repoPath(owner, repo), index),
		nil,
		nil,
	)
}

// PullRequestMerge merges a pull request.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoMergePullRequest
func (c *Client) PullRequestMerge(
	ctx context.Context,
	owner string,
	repo string,
	index int64,
	opt *MergePullRequestOption,
) (*PullRequest, *Response, error) {
	resp, err := c.post(
		ctx,
		fmt.Sprintf("%s/pulls/%d/merge", repoPath(owner, repo), index),
		nil,
		opt,
		nil,
	)
	if err != nil {
		return nil, resp, err
	}
	return nil, resp, nil
}

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

// CombinedStatusGet fetches the combined status for a ref.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetCombinedStatusByRef
func (c *Client) CombinedStatusGet(
	ctx context.Context,
	owner string,
	repo string,
	ref string,
) (*CombinedStatus, *Response, error) {
	var response CombinedStatus
	resp, err := c.get(
		ctx,
		fmt.Sprintf("%s/commits/%s/status", repoPath(owner, repo), url.PathEscape(ref)),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommitStatusList lists statuses for a commit SHA.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoListStatuses
func (c *Client) CommitStatusList(
	ctx context.Context,
	owner string,
	repo string,
	sha string,
	opt *ListOptions,
) ([]*CommitStatus, *Response, error) {
	var response []*CommitStatus
	resp, err := c.get(
		ctx,
		fmt.Sprintf("%s/statuses/%s", repoPath(owner, repo), url.PathEscape(sha)),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// CommitStatusCreate creates a commit status.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoCreateStatus
func (c *Client) CommitStatusCreate(
	ctx context.Context,
	owner string,
	repo string,
	sha string,
	opt *CreateStatusOption,
) (*CommitStatus, *Response, error) {
	var response CommitStatus
	resp, err := c.post(
		ctx,
		fmt.Sprintf("%s/statuses/%s", repoPath(owner, repo), url.PathEscape(sha)),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// IssueTemplateList lists issue templates.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetIssueTemplates
func (c *Client) IssueTemplateList(
	ctx context.Context,
	owner string,
	repo string,
) ([]*IssueTemplate, *Response, error) {
	var response []*IssueTemplate
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/issue_templates",
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// ContentsGet fetches repository contents.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetContents
func (c *Client) ContentsGet(
	ctx context.Context,
	owner string,
	repo string,
	filepath string,
) (*ContentsResponse, *Response, error) {
	var response ContentsResponse
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/contents/"+url.PathEscape(filepath),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// ContentsList lists repository contents under a directory.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetContents
func (c *Client) ContentsList(
	ctx context.Context,
	owner string,
	repo string,
	filepath string,
) ([]*ContentsResponse, *Response, error) {
	var response []*ContentsResponse
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/contents/"+url.PathEscape(filepath),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// LabelList lists repository labels.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/issue/issueListLabels
func (c *Client) LabelList(
	ctx context.Context,
	owner string,
	repo string,
	opt *ListOptions,
) ([]*Label, *Response, error) {
	var response []*Label
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/labels",
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// LabelCreate creates a repository label.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/issue/issueCreateLabel
func (c *Client) LabelCreate(
	ctx context.Context,
	owner string,
	repo string,
	opt *CreateLabelOption,
) (*Label, *Response, error) {
	var response Label
	resp, err := c.post(
		ctx,
		repoPath(owner, repo)+"/labels",
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
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

// AssigneeList lists users who can be assigned to issues.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/repository/repoGetAssignees
func (c *Client) AssigneeList(
	ctx context.Context,
	owner string,
	repo string,
	opt *ListOptions,
) ([]*User, *Response, error) {
	var response []*User
	resp, err := c.get(
		ctx,
		repoPath(owner, repo)+"/assignees",
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// UserSearch searches Forgejo users.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/user/userSearch
func (c *Client) UserSearch(
	ctx context.Context,
	opt *UserSearchOptions,
) (*UserSearchResults, *Response, error) {
	var response UserSearchResults
	resp, err := c.get(ctx, "users/search", opt.encodeQuery(), &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// ListOptions configures Forgejo page-based pagination.
type ListOptions struct {
	// Page selects the one-based result page.
	Page int64

	// Limit selects the maximum number of items per page.
	Limit int64
}

func (o *ListOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.Page > 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	if o.Limit > 0 {
		values.Set("limit", strconv.FormatInt(o.Limit, 10))
	}
	return values
}

// PullRequestListOptions configures pull request listing.
type PullRequestListOptions struct {
	ListOptions

	// State filters by pull request state.
	State string

	// Sort selects the server sort key.
	Sort string

	// Milestone filters by milestone ID.
	Milestone int64

	// Labels filters by comma-delimited label IDs.
	Labels string
}

func (o *PullRequestListOptions) encodeQuery() url.Values {
	if o == nil {
		return make(url.Values)
	}
	values := o.ListOptions.encodeQuery()
	if o.State != "" {
		values.Set("state", o.State)
	}
	if o.Sort != "" {
		values.Set("sort", o.Sort)
	}
	if o.Milestone > 0 {
		values.Set("milestone", strconv.FormatInt(o.Milestone, 10))
	}
	if o.Labels != "" {
		values.Set("labels", o.Labels)
	}
	return values
}

// UserSearchOptions configures user search.
type UserSearchOptions struct {
	ListOptions

	// Query is the username or display-name search string.
	Query string
}

func (o *UserSearchOptions) encodeQuery() url.Values {
	if o == nil {
		return make(url.Values)
	}
	values := o.ListOptions.encodeQuery()
	if o.Query != "" {
		values.Set("q", o.Query)
	}
	return values
}

// Repository matches the subset of the Forgejo repository response
// used by the forge.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/Repository
type Repository struct {
	// ID is the repository's numeric ID.
	ID int64 `json:"id"`

	// Name is the repository name without owner.
	Name string `json:"name"`

	// FullName is the owner-qualified repository name.
	FullName string `json:"full_name"`

	// HTMLURL is the repository web URL.
	HTMLURL string `json:"html_url"`

	// Permissions reports the authenticated user's repository permissions.
	Permissions *Permission `json:"permissions"`
}

// Permission matches Forgejo repository permission flags.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/Permission
type Permission struct {
	// Admin is true if the user has repository administration access.
	Admin bool `json:"admin"`

	// Pull is true if the user can read repository contents.
	Pull bool `json:"pull"`

	// Push is true if the user can push repository contents.
	Push bool `json:"push"`
}

// User matches the subset of Forgejo user fields the forge uses.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/User
type User struct {
	// ID is the user's numeric ID.
	ID int64 `json:"id"`

	// Login is the user's login name.
	Login string `json:"login"`

	// FullName is the user's display name.
	FullName string `json:"full_name"`

	// UserName is an alternate username field used by some responses.
	UserName string `json:"username"`
}

// PullRequest matches the subset of Forgejo pull request fields
// used by the forge.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/PullRequest
type PullRequest struct {
	// ID is the pull request's database ID.
	ID int64 `json:"id"`

	// Index is the repository-local pull request number.
	Index int64 `json:"number"`

	// URL is the API URL for the pull request.
	URL string `json:"url"`

	// HTMLURL is the web URL for the pull request.
	HTMLURL string `json:"html_url"`

	// State is the pull request state.
	State string `json:"state"`

	// Title is the pull request title.
	Title string `json:"title"`

	// Body is the pull request body.
	Body string `json:"body"`

	// User is the pull request author.
	User *User `json:"user"`

	// Head identifies the source branch.
	Head *PRBranchInfo `json:"head"`

	// Base identifies the target branch.
	Base *PRBranchInfo `json:"base"`

	// Mergeable reports whether Forgejo considers the pull request mergeable.
	Mergeable bool `json:"mergeable"`

	// Merged reports whether the pull request is merged.
	Merged bool `json:"merged"`

	// MergedCommitID is the merge commit SHA if the pull request was merged.
	MergedCommitID string `json:"merge_commit_sha"`

	// Draft reports whether the pull request is a draft.
	Draft bool `json:"draft"`

	// Labels lists labels attached to the pull request issue.
	Labels []*Label `json:"labels"`

	// Assignees lists users assigned to the pull request issue.
	Assignees []*User `json:"assignees"`

	// RequestedReviewers lists users requested for review.
	RequestedReviewers []*User `json:"requested_reviewers"`
}

// PRBranchInfo matches Forgejo pull request branch metadata.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/PRBranchInfo
type PRBranchInfo struct {
	// Label is Forgejo's owner-qualified branch label.
	Label string `json:"label"`

	// Ref is the branch ref name.
	Ref string `json:"ref"`

	// SHA is the current commit SHA.
	SHA string `json:"sha"`

	// RepoID is the branch repository's numeric ID.
	RepoID int64 `json:"repo_id"`

	// Repository is the branch repository object.
	Repository *Repository `json:"repo"`
}

// CreatePullRequestOption is the request body for creating a pull request.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CreatePullRequestOption
type CreatePullRequestOption struct {
	// Title is the pull request title.
	Title string `json:"title"`

	// Body is the pull request body.
	Body string `json:"body,omitempty"`

	// Head is the source branch.
	Head string `json:"head"`

	// Base is the target branch.
	Base string `json:"base"`

	// Assignee is the login of a single assignee.
	Assignee string `json:"assignee,omitempty"`

	// Assignees are assignee logins.
	Assignees []string `json:"assignees,omitempty"`

	// Labels are label IDs.
	Labels []int64 `json:"labels,omitempty"`

	// Milestone is the milestone ID.
	Milestone int64 `json:"milestone,omitempty"`

	// Draft requests a draft pull request.
	Draft bool `json:"draft,omitempty"`
}

// EditPullRequestOption is the request body for editing a pull request.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/EditPullRequestOption
type EditPullRequestOption struct {
	// Title updates the pull request title.
	Title *string `json:"title,omitempty"`

	// Body updates the pull request body.
	Body *string `json:"body,omitempty"`

	// Base updates the target branch.
	Base *string `json:"base,omitempty"`

	// Assignee updates the single assignee.
	Assignee *string `json:"assignee,omitempty"`

	// Assignees updates assignee logins.
	Assignees *[]string `json:"assignees,omitempty"`

	// Labels updates label IDs.
	Labels *[]int64 `json:"labels,omitempty"`

	// Milestone updates the milestone ID.
	Milestone *int64 `json:"milestone,omitempty"`

	// State updates the pull request state.
	State *string `json:"state,omitempty"`

	// Draft updates the pull request draft state.
	Draft *bool `json:"draft,omitempty"`
}

// MergePullRequestOption is the request body for merging a pull request.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/MergePullRequestOption
type MergePullRequestOption struct {
	// Do selects the Forgejo merge operation.
	Do string `json:"Do"`

	// HeadCommitID requires the pull request head to match before merging.
	HeadCommitID string `json:"head_commit_id,omitempty"`

	// MergeTitleField sets the merge commit title.
	MergeTitleField string `json:"MergeTitleField,omitempty"`

	// MergeMessageField sets the merge commit message.
	MergeMessageField string `json:"MergeMessageField,omitempty"`

	// DeleteBranchAfterMerge deletes the source branch after merge.
	DeleteBranchAfterMerge bool `json:"delete_branch_after_merge,omitempty"`

	// ForceMerge allows a forced merge when Forgejo supports it.
	ForceMerge bool `json:"force_merge,omitempty"`
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

// CombinedStatus matches a Forgejo combined commit status.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CombinedStatus
type CombinedStatus struct {
	// State is the aggregate commit status.
	State CommitStatusState `json:"state"`

	// SHA is the commit SHA.
	SHA string `json:"sha"`

	// Statuses lists individual commit statuses.
	Statuses []*CommitStatus `json:"statuses"`
}

// CommitStatusState is a Forgejo commit status state.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CommitStatusState
type CommitStatusState string

// Forgejo commit status values.
const (
	// CommitStatusPending reports a pending status.
	CommitStatusPending CommitStatusState = "pending"

	// CommitStatusSuccess reports a successful status.
	CommitStatusSuccess CommitStatusState = "success"

	// CommitStatusError reports an errored status.
	CommitStatusError CommitStatusState = "error"

	// CommitStatusFailure reports a failed status.
	CommitStatusFailure CommitStatusState = "failure"

	// CommitStatusWarning reports a warning status.
	CommitStatusWarning CommitStatusState = "warning"
)

// CommitStatus matches a Forgejo commit status.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CommitStatus
type CommitStatus struct {
	// ID is the status ID.
	ID int64 `json:"id"`

	// State is the commit status state.
	State CommitStatusState `json:"status"`

	// TargetURL is the URL for status details.
	TargetURL string `json:"target_url"`

	// Description describes the status.
	Description string `json:"description"`

	// Context names the status context.
	Context string `json:"context"`
}

// CreateStatusOption is the request body for creating a commit status.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CreateStatusOption
type CreateStatusOption struct {
	// State is the commit status state.
	State CommitStatusState `json:"state"`

	// TargetURL is the URL for status details.
	TargetURL string `json:"target_url,omitempty"`

	// Description describes the status.
	Description string `json:"description,omitempty"`

	// Context names the status context.
	Context string `json:"context,omitempty"`
}

// IssueTemplate matches a Forgejo issue template.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/IssueTemplate
type IssueTemplate struct {
	// Name is the template display name.
	Name string `json:"name"`

	// Title is the default issue title.
	Title string `json:"title"`

	// Content is the template body.
	Content string `json:"content"`

	// FileName is the template file name.
	FileName string `json:"file_name"`
}

// ContentsResponse matches a Forgejo contents response.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/ContentsResponse
type ContentsResponse struct {
	// Name is the base file name.
	Name string `json:"name"`

	// Path is the repository-relative path.
	Path string `json:"path"`

	// Type is the content type.
	Type string `json:"type"`

	// Content is the base64-encoded file content.
	Content string `json:"content"`

	// Encoding is the content encoding.
	Encoding string `json:"encoding"`

	// DownloadURL is the raw download URL.
	DownloadURL string `json:"download_url"`
}

// Label matches a Forgejo label.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/Label
type Label struct {
	// ID is the label ID.
	ID int64 `json:"id"`

	// Name is the label name.
	Name string `json:"name"`

	// Color is the label color.
	Color string `json:"color"`

	// Description is the label description.
	Description string `json:"description"`
}

// CreateLabelOption is the request body for creating a label.
//
// Forgejo API:
// https://codeberg.org/swagger.v1.json#/definitions/CreateLabelOption
type CreateLabelOption struct {
	// Name is the label name.
	Name string `json:"name"`

	// Color is the hex color without a leading "#".
	Color string `json:"color,omitempty"`

	// Description is the label description.
	Description string `json:"description,omitempty"`
}

// UserSearchResults matches the Forgejo user search response.
//
// Forgejo API:
// https://codeberg.org/api/swagger#/user/userSearch
type UserSearchResults struct {
	// OK reports whether the search succeeded.
	OK bool `json:"ok"`

	// Data lists matching users.
	Data []*User `json:"data"`
}

func repoPath(owner string, repo string) string {
	return fmt.Sprintf(
		"repos/%s/%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
	)
}
