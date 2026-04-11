package bitbucket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Bitbucket Cloud REST API references:
// - Pull requests and pull request comments:
//   https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/
// - Workspace members:
//   https://developer.atlassian.com/cloud/bitbucket/rest/api-group-workspaces/

const (
	defaultPullRequestPageLen = 10
	defaultCommentPageLen     = 100
)

// PullRequest is a Bitbucket pull request.
type PullRequest struct {
	ID          int64            `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	State       string           `json:"state"`
	Draft       bool             `json:"draft"`
	Source      BranchRef        `json:"source"`
	Destination BranchRef        `json:"destination"`
	Reviewers   []User           `json:"reviewers"`
	Links       PullRequestLinks `json:"links"`
	MergeCommit *Commit          `json:"merge_commit,omitempty"`
}

// PullRequestCreateRequest is the request body for creating a pull request.
type PullRequestCreateRequest struct {
	Title             string     `json:"title"`
	Description       string     `json:"description,omitempty"`
	Source            BranchRef  `json:"source"`
	Destination       BranchRef  `json:"destination"`
	Reviewers         []Reviewer `json:"reviewers,omitempty"`
	CloseSourceBranch bool       `json:"close_source_branch,omitempty"`
	Draft             bool       `json:"draft,omitempty"`
}

// PullRequestUpdateRequest is the request body for updating a pull request.
type PullRequestUpdateRequest struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Destination *BranchRef `json:"destination,omitempty"`
	Reviewers   []Reviewer `json:"reviewers,omitempty"`
	Draft       *bool      `json:"draft,omitempty"`
}

// PullRequestList is the paginated response for listing pull requests.
type PullRequestList struct {
	Values []PullRequest `json:"values"`
	Next   string        `json:"next,omitempty"`
}

// PullRequestListOptions controls pull request listing and pagination.
type PullRequestListOptions struct {
	Query   string
	PageLen int
	Fields  []string
	PageURL string
}

// Comment is a Bitbucket pull request comment.
type Comment struct {
	ID         int64       `json:"id"`
	Content    Content     `json:"content"`
	Inline     *Inline     `json:"inline,omitempty"`
	Resolution *Resolution `json:"resolution,omitempty"`
}

// CommentCreateRequest is the request body for creating or updating a comment.
type CommentCreateRequest struct {
	Content Content `json:"content"`
}

// CommentList is the paginated response for listing comments.
type CommentList struct {
	Values []Comment `json:"values"`
	Next   string    `json:"next,omitempty"`
}

// CommentListOptions controls comment listing and pagination.
type CommentListOptions struct {
	PageLen int
	PageURL string
}

// WorkspaceMember is a Bitbucket workspace member.
type WorkspaceMember struct {
	User User `json:"user"`
}

// WorkspaceMemberList is the paginated response
// for listing workspace members.
type WorkspaceMemberList struct {
	Values []WorkspaceMember `json:"values"`
	Next   string            `json:"next,omitempty"`
}

// WorkspaceMemberListOptions controls workspace member pagination.
type WorkspaceMemberListOptions struct {
	PageURL string
}

// BranchRef references a branch in a repository.
type BranchRef struct {
	Branch Branch  `json:"branch"`
	Commit *Commit `json:"commit,omitempty"`
}

// Branch is a branch reference within a request or response.
type Branch struct {
	Name string `json:"name"`
}

// Reviewer identifies a reviewer by UUID.
type Reviewer struct {
	UUID string `json:"uuid"`
}

// User is a Bitbucket user.
type User struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AccountID   string `json:"account_id"`
	Nickname    string `json:"nickname"`
}

// Commit is a Bitbucket commit reference.
type Commit struct {
	Hash string `json:"hash"`
}

// PullRequestLinks contains links associated with a pull request.
type PullRequestLinks struct {
	HTML Link `json:"html"`
}

// Link is a Bitbucket hyperlink.
type Link struct {
	Href string `json:"href"`
}

// Content contains raw comment text.
type Content struct {
	Raw string `json:"raw"`
}

// Inline identifies an inline comment location.
type Inline struct {
	Path string `json:"path"`
	From *int   `json:"from,omitempty"`
	To   *int   `json:"to,omitempty"`
}

// Resolution indicates a comment resolution state.
type Resolution struct {
	Type string `json:"type"`
}

// PullRequestCreate creates a pull request.
func (c *Client) PullRequestCreate(
	ctx context.Context,
	workspace string,
	repo string,
	req *PullRequestCreateRequest,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.post(
		ctx,
		fmt.Sprintf("/repositories/%s/%s/pullrequests", workspace, repo),
		nil,
		req,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestGet fetches a pull request by ID.
func (c *Client) PullRequestGet(
	ctx context.Context,
	workspace string,
	repo string,
	prID int64,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", workspace, repo, prID),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestUpdate updates a pull request.
func (c *Client) PullRequestUpdate(
	ctx context.Context,
	workspace string,
	repo string,
	prID int64,
	req *PullRequestUpdateRequest,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.put(
		ctx,
		fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", workspace, repo, prID),
		nil,
		req,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestList lists pull requests.
func (c *Client) PullRequestList(
	ctx context.Context,
	workspace string,
	repo string,
	opt *PullRequestListOptions,
) (*PullRequestList, *Response, error) {
	resourcePath, query := buildPullRequestListRequest(workspace, repo, opt)

	var response PullRequestList
	resp, err := c.get(ctx, resourcePath, query, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentCreate creates a pull request comment.
func (c *Client) CommentCreate(
	ctx context.Context,
	workspace string,
	repo string,
	prID int64,
	req *CommentCreateRequest,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.post(
		ctx,
		fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/comments", workspace, repo, prID),
		nil,
		req,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentUpdate updates a pull request comment.
func (c *Client) CommentUpdate(
	ctx context.Context,
	workspace string,
	repo string,
	prID int64,
	commentID int64,
	req *CommentCreateRequest,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.put(
		ctx,
		fmt.Sprintf(
			"/repositories/%s/%s/pullrequests/%d/comments/%d",
			workspace,
			repo,
			prID,
			commentID,
		),
		nil,
		req,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentDelete deletes a pull request comment.
func (c *Client) CommentDelete(
	ctx context.Context,
	workspace string,
	repo string,
	prID int64,
	commentID int64,
) (*Response, error) {
	return c.delete(
		ctx,
		fmt.Sprintf(
			"/repositories/%s/%s/pullrequests/%d/comments/%d",
			workspace,
			repo,
			prID,
			commentID,
		),
		nil,
	)
}

// CommentList lists pull request comments.
func (c *Client) CommentList(
	ctx context.Context,
	workspace string,
	repo string,
	prID int64,
	opt *CommentListOptions,
) (*CommentList, *Response, error) {
	resourcePath, query := buildCommentListRequest(workspace, repo, prID, opt)

	var response CommentList
	resp, err := c.get(ctx, resourcePath, query, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// WorkspaceMemberList lists workspace members.
func (c *Client) WorkspaceMemberList(
	ctx context.Context,
	workspace string,
	opt *WorkspaceMemberListOptions,
) (*WorkspaceMemberList, *Response, error) {
	resourcePath := buildWorkspaceMemberListPath(workspace, opt)

	var response WorkspaceMemberList
	resp, err := c.get(ctx, resourcePath, nil, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

func buildPullRequestListRequest(
	workspace string,
	repo string,
	opt *PullRequestListOptions,
) (string, url.Values) {
	if opt != nil && opt.PageURL != "" {
		return opt.PageURL, nil
	}

	pageLen := defaultPullRequestPageLen
	if opt != nil && opt.PageLen > 0 {
		pageLen = opt.PageLen
	}

	var queryParts []string
	if opt != nil && opt.Query != "" {
		queryParts = append(queryParts, "q="+url.QueryEscape(opt.Query))
	}
	queryParts = append(queryParts, fmt.Sprintf("pagelen=%d", pageLen))
	if opt != nil && len(opt.Fields) > 0 {
		queryParts = append(
			queryParts,
			"fields="+url.QueryEscape(strings.Join(opt.Fields, ",")),
		)
	}

	resourcePath := fmt.Sprintf("/repositories/%s/%s/pullrequests", workspace, repo)
	if len(queryParts) > 0 {
		resourcePath += "?" + strings.Join(queryParts, "&")
	}

	return resourcePath, nil
}

func buildCommentListRequest(
	workspace string,
	repo string,
	prID int64,
	opt *CommentListOptions,
) (string, url.Values) {
	if opt != nil && opt.PageURL != "" {
		return opt.PageURL, nil
	}

	query := make(url.Values)
	pageLen := defaultCommentPageLen
	if opt != nil && opt.PageLen > 0 {
		pageLen = opt.PageLen
	}
	query.Set("pagelen", strconv.Itoa(pageLen))

	return fmt.Sprintf(
		"/repositories/%s/%s/pullrequests/%d/comments",
		workspace,
		repo,
		prID,
	), query
}

func buildWorkspaceMemberListPath(
	workspace string,
	opt *WorkspaceMemberListOptions,
) string {
	if opt != nil && opt.PageURL != "" {
		return opt.PageURL
	}
	return fmt.Sprintf("/workspaces/%s/members", workspace)
}
