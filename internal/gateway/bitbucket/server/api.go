package server

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"strings"
)

// Bitbucket Data Center/Server REST 1.0 API references:
//   - Pull requests, comments, and activities:
//     https://developer.atlassian.com/server/bitbucket/rest/v904/api-group-pull-requests/
//   - Build status:
//     https://developer.atlassian.com/server/bitbucket/rest/v904/api-group-builds-and-deployments/

// PullRequest is a Bitbucket Data Center pull request. Not all fields are
// populated by every endpoint.
type PullRequest struct {
	ID int64 `json:"id"`

	// Version is the optimistic-locking version, required on update and merge.
	Version int `json:"version"`

	Title       string `json:"title"`
	Description string `json:"description"`

	// State is one of "OPEN", "MERGED", or "DECLINED".
	State string `json:"state"`

	Draft bool `json:"draft"`

	FromRef Ref `json:"fromRef"` // source (head)
	ToRef   Ref `json:"toRef"`   // destination (base)

	Author    Author     `json:"author"`
	Reviewers []Reviewer `json:"reviewers"`
	Links     Links      `json:"links"`
}

// Ref references a branch within a pull request.
type Ref struct {
	// DisplayID is the short ref name, e.g. "main".
	DisplayID    string `json:"displayId"`
	LatestCommit string `json:"latestCommit"`
}

// Author is the author entry on a pull request.
type Author struct {
	User User `json:"user"`
}

// Reviewer is a reviewer entry on a pull request.
type Reviewer struct {
	User User `json:"user"`
}

// User is a Bitbucket Data Center user. Name is the username used to address
// users in API requests.
type User struct {
	Name string `json:"name"`
}

// Links holds hyperlinks associated with a pull request.
type Links struct {
	Self []Link `json:"self"`
}

// Link is a single hyperlink.
type Link struct {
	Href string `json:"href"`
}

// PullRequestCreateRequest is the request body for creating a pull request.
type PullRequestCreateRequest struct {
	Title       string           `json:"title"` // required
	Description string           `json:"description,omitempty"`
	FromRef     CreateRef        `json:"fromRef"` // required
	ToRef       CreateRef        `json:"toRef"`   // required
	Reviewers   []CreateReviewer `json:"reviewers,omitempty"`

	// Draft was added in Data Center 8.18; older servers ignore it.
	Draft bool `json:"draft,omitempty"`
}

// CreateRef references a branch in a repository on pull request creation.
type CreateRef struct {
	ID         string              `json:"id"`         // required
	Repository CreateRefRepository `json:"repository"` // required
}

// CreateRefRepository identifies a repository in a pull request create ref.
type CreateRefRepository struct {
	Slug    string           `json:"slug"`    // required
	Project CreateRefProject `json:"project"` // required
}

// CreateRefProject identifies a project in a pull request create ref.
type CreateRefProject struct {
	Key string `json:"key"` // required
}

// CreateReviewer requests a reviewer on pull request creation.
type CreateReviewer struct {
	User CreateReviewerUser `json:"user"` // required
}

// CreateReviewerUser identifies a reviewer by username.
type CreateReviewerUser struct {
	Name string `json:"name"` // required
}

// PullRequestCreate creates a pull request.
func (c *Client) PullRequestCreate(
	ctx context.Context,
	projectKey string,
	slug string,
	req PullRequestCreateRequest,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.post(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
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

// PullRequestGet fetches a pull request by ID, fully populated. A missing
// pull request maps to [ErrNotFound].
func (c *Client) PullRequestGet(
	ctx context.Context,
	projectKey string,
	slug string,
	id int64,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			id,
		),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// PullRequestListRequest filters a pull request listing. Empty fields are
// omitted so the server applies its default.
type PullRequestListRequest struct {
	// At is a fully qualified ref (e.g. "refs/heads/feature"); with
	// Direction OUTGOING it lists pull requests opened from that branch.
	At string

	// Direction is "OUTGOING" (At is the source) or "INCOMING" (At is the target).
	Direction string

	// State is "OPEN", "MERGED", "DECLINED", or "ALL".
	State string
}

// PullRequestList lists pull requests in the repository, following pagination.
// Iteration stops on the first error, yielding the zero [PullRequest].
func (c *Client) PullRequestList(
	ctx context.Context,
	projectKey string,
	slug string,
	req PullRequestListRequest,
) iter.Seq2[PullRequest, error] {
	query := make(url.Values, 3)
	if req.At != "" {
		query.Set("at", req.At)
	}
	if req.Direction != "" {
		query.Set("direction", req.Direction)
	}
	if req.State != "" {
		query.Set("state", req.State)
	}

	return getPaged[PullRequest](
		ctx,
		c,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
		),
		query,
	)
}

// PullRequestUpdateRequest is the request body for updating a pull request.
// Data Center replaces the mutable fields wholesale under the optimistic-
// locking Version, so fetch the current pull request first (see
// [Client.PullRequestGet]) to preserve fields you don't intend to change.
type PullRequestUpdateRequest struct {
	// Version must match the server's current version or the update fails
	// with [ErrConflict].
	Version int `json:"version"`

	Title string `json:"title"` // required

	// Description, when non-nil, replaces the description ("" clears it).
	Description *string `json:"description,omitempty"`

	// Reviewers, when non-nil, replaces the reviewers.
	Reviewers []CreateReviewer `json:"reviewers,omitempty"`

	// ToRef, when non-nil, changes the base ref (by fully qualified ref ID).
	ToRef *UpdateRef `json:"toRef,omitempty"`
}

// UpdateRef references a branch when updating a pull request's base. Only the
// fully qualified ref ID is needed; the repository is taken from the PR.
type UpdateRef struct {
	ID string `json:"id"` // required, e.g. "refs/heads/main"
}

// PullRequestUpdate updates a pull request's mutable fields. A stale Version
// maps to [ErrConflict]; refetch and retry.
func (c *Client) PullRequestUpdate(
	ctx context.Context,
	projectKey string,
	slug string,
	id int64,
	req PullRequestUpdateRequest,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.put(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			id,
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

// PullRequestMergeRequest is the request body for merging a pull request.
type PullRequestMergeRequest struct {
	// StrategyID is a repository merge strategy (e.g. "no-ff", "squash",
	// "rebase-no-ff"); empty uses the repository default.
	StrategyID string `json:"strategyId,omitempty"`
}

// PullRequestMerge merges an open pull request. The version is a query
// parameter; a stale version maps to [ErrConflict].
func (c *Client) PullRequestMerge(
	ctx context.Context,
	projectKey string,
	slug string,
	id int64,
	version int,
	req PullRequestMergeRequest,
) (*PullRequest, *Response, error) {
	var response PullRequest
	resp, err := c.post(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/merge",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			id,
		),
		url.Values{"version": []string{strconv.Itoa(version)}},
		req,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeStatus is the result of GET .../pull-requests/{id}/merge.
type MergeStatus struct {
	CanMerge   bool `json:"canMerge"`
	Conflicted bool `json:"conflicted"`

	// Outcome is "CLEAN", "CONFLICTED", or "UNKNOWN".
	Outcome string `json:"outcome"`

	// Vetoes are the reasons the pull request cannot be merged.
	Vetoes []Veto `json:"vetoes"`
}

// Veto is a single reason a pull request cannot be merged.
type Veto struct {
	SummaryMessage  string `json:"summaryMessage"`
	DetailedMessage string `json:"detailedMessage"`
}

// PullRequestCanMerge reports whether a pull request can be merged, including
// any vetoes blocking the merge.
func (c *Client) PullRequestCanMerge(
	ctx context.Context,
	projectKey string,
	slug string,
	id int64,
) (*MergeStatus, *Response, error) {
	var response MergeStatus
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/merge",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			id,
		),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CurrentUser identifies the authenticated user. Data Center has no "/user"
// endpoint, so the identity comes from the X-AUSERNAME response header.
type CurrentUser struct {
	Name string
}

// CurrentUser fetches the authenticated user's identity from the X-AUSERNAME
// response header, so it works even for tokens lacking broad read permissions.
func (c *Client) CurrentUser(ctx context.Context) (*CurrentUser, *Response, error) {
	// application-properties needs no permissions and never 404s, yet still
	// returns the X-AUSERNAME header identifying the caller.
	resp, err := c.get(ctx, "/application-properties", nil, nil)
	if err != nil {
		return nil, resp, err
	}

	if resp == nil || resp.AUserName == "" {
		return nil, resp, errors.New("server did not return X-AUSERNAME header")
	}

	return &CurrentUser{Name: resp.AUserName}, resp, nil
}

// ApplicationProperties describes the running Bitbucket Data Center server.
type ApplicationProperties struct {
	// Version is the server version, e.g. "9.4.0".
	Version string `json:"version"`

	// BuildNumber encodes the version as MAJOR*1e6+MINOR*1e3+PATCH
	// (e.g. "9004000"), a fallback when Version is not valid semver.
	BuildNumber string `json:"buildNumber"`
}

// ApplicationProperties fetches the server descriptor for feature gating.
// The result is memoized on success only, so a transient failure is retried.
func (c *Client) ApplicationProperties(ctx context.Context) (*ApplicationProperties, error) {
	c.appPropsMu.Lock()
	defer c.appPropsMu.Unlock()

	if c.appProps != nil {
		return c.appProps, nil
	}

	var props ApplicationProperties
	if _, err := c.get(ctx, "/application-properties", nil, &props); err != nil {
		return nil, err
	}

	c.appProps = &props
	return &props, nil
}

// BuildStatus is a single CI build status reported for a commit.
type BuildStatus struct {
	Key   string `json:"key"`
	State string `json:"state"` // one of the BuildStatus* constants
}

// Bitbucket Data Center build status states.
const (
	BuildStatusSuccessful = "SUCCESSFUL"
	BuildStatusInProgress = "INPROGRESS"
	BuildStatusFailed     = "FAILED"
)

// BuildStatusList lists all CI build statuses for a commit, following
// pagination. It targets the build-status REST API root.
func (c *Client) BuildStatusList(
	ctx context.Context,
	commitID string,
) ([]BuildStatus, error) {
	var statuses []BuildStatus
	for status, err := range getPaged[BuildStatus](
		ctx, c, c.buildBase+"/commits/"+url.PathEscape(commitID), nil,
	) {
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Repository is a Bitbucket Data Center repository descriptor. The numeric ID
// is required by the default-reviewers endpoint.
type Repository struct {
	ID int64 `json:"id"`
}

// RepositoryGet fetches a repository by project key and slug. A missing
// repository maps to [ErrNotFound].
func (c *Client) RepositoryGet(
	ctx context.Context,
	projectKey string,
	slug string,
) (*Repository, *Response, error) {
	var response Repository
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
		),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// RawFileGet fetches the raw contents of the file at the given path
// on the repository's default branch.
// A missing file maps to [ErrNotFound].
func (c *Client) RawFileGet(
	ctx context.Context,
	projectKey string,
	slug string,
	path string,
) ([]byte, *Response, error) {
	return c.getRaw(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/raw/%s",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			escapePath(path),
		),
	)
}

// escapePath escapes each segment of a slash-separated path,
// preserving the separators.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// DefaultReviewer is one entry from the default-reviewers endpoint.
type DefaultReviewer struct {
	Name string `json:"name"`
}

// DefaultReviewers returns the users configured as default (required)
// reviewers for a sourceRefID->targetRefID pull request. Refs are fully
// qualified, and sourceRepoID equals targetRepoID for same-repository PRs.
func (c *Client) DefaultReviewers(
	ctx context.Context,
	projectKey string,
	slug string,
	sourceRepoID int64,
	targetRepoID int64,
	sourceRefID string,
	targetRefID string,
) ([]DefaultReviewer, *Response, error) {
	query := url.Values{
		"sourceRepoId": []string{strconv.FormatInt(sourceRepoID, 10)},
		"targetRepoId": []string{strconv.FormatInt(targetRepoID, 10)},
		"sourceRefId":  []string{sourceRefID},
		"targetRefId":  []string{targetRefID},
	}

	var response []DefaultReviewer
	resp, err := c.get(
		ctx,
		c.defaultReviewersBase+fmt.Sprintf(
			"/projects/%s/repos/%s/reviewers",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
		),
		query,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// Comment is a Bitbucket Data Center pull request comment. Update and delete
// are optimistic-locking: a stale Version fails with [ErrConflict].
type Comment struct {
	ID int64 `json:"id"`

	// Version is the optimistic-locking version, required on update and delete.
	Version int `json:"version"`

	Text   string `json:"text"`
	Author User   `json:"author"`

	// Severity is "NORMAL" or "BLOCKER" (a task; Data Center folds tasks
	// into comments as blocker-severity comments).
	Severity string `json:"severity"`

	// State is "OPEN", "RESOLVED", or "PENDING" (an unpublished draft). For a
	// task, "RESOLVED" means completed.
	State string `json:"state"`

	// ThreadResolved reports whether the thread was resolved via the "Resolve"
	// action, independent of task state.
	ThreadResolved bool `json:"threadResolved"`
}

// commentText is the request body shared by comment create and update.
type commentText struct {
	Text    string `json:"text"`              // required
	Version *int   `json:"version,omitempty"` // update only
}

// CommentCreate adds a top-level comment to a pull request. The returned
// [Comment] carries the new ID and initial Version.
func (c *Client) CommentCreate(
	ctx context.Context,
	projectKey string,
	slug string,
	prID int64,
	text string,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.post(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/comments",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			prID,
		),
		nil,
		commentText{Text: text},
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentUpdate edits the text of a pull request comment. A stale version
// maps to [ErrConflict]; refetch the current version and retry.
func (c *Client) CommentUpdate(
	ctx context.Context,
	projectKey string,
	slug string,
	prID int64,
	commentID int64,
	text string,
	version int,
) (*Comment, *Response, error) {
	var response Comment
	resp, err := c.put(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/comments/%d",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			prID,
			commentID,
		),
		nil,
		commentText{Text: text, Version: &version},
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// CommentDelete removes a pull request comment. The version is a query
// parameter; a missing comment maps to [ErrNotFound], a stale one to [ErrConflict].
func (c *Client) CommentDelete(
	ctx context.Context,
	projectKey string,
	slug string,
	prID int64,
	commentID int64,
	version int,
) (*Response, error) {
	return c.delete(
		ctx,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/comments/%d",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			prID,
			commentID,
		),
		url.Values{"version": []string{strconv.Itoa(version)}},
	)
}

// Activity is a single entry in a pull request's activity feed, which
// git-spice reads to reconstruct comments.
type Activity struct {
	// Action is the activity kind, e.g. [ActivityActionCommented], "OPENED",
	// or "MERGED".
	Action string `json:"action"`

	// Comment is set only when Action is [ActivityActionCommented].
	Comment *Comment `json:"comment"`
}

// Bitbucket Data Center pull request activity actions.
const (
	ActivityActionCommented = "COMMENTED"
)

// ActivityList iterates a pull request's activity feed, following pagination.
// Iteration stops on the first error, yielding the zero [Activity].
func (c *Client) ActivityList(
	ctx context.Context,
	projectKey string,
	slug string,
	prID int64,
) iter.Seq2[Activity, error] {
	return getPaged[Activity](
		ctx,
		c,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/activities",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			prID,
		),
		nil,
	)
}

// BlockerCommentList iterates a pull request's blocker-severity comments (its
// tasks) as a flat list, following pagination. Unlike [Client.ActivityList],
// it returns tasks at any nesting depth, including replies. A task's
// completion is reported by its State ("RESOLVED").
//
// Requires Data Center 7.2+; older servers respond 404 ([ErrNotFound]).
func (c *Client) BlockerCommentList(
	ctx context.Context,
	projectKey string,
	slug string,
	prID int64,
) iter.Seq2[Comment, error] {
	return getPaged[Comment](
		ctx,
		c,
		fmt.Sprintf(
			"/projects/%s/repos/%s/pull-requests/%d/blocker-comments",
			url.PathEscape(projectKey),
			url.PathEscape(slug),
			prID,
		),
		nil,
	)
}
