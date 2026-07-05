package forgejo

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

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
