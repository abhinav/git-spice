package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ProjectGet fetches a single project by numeric ID or `group/project` path.
//
// GitLab API:
// https://docs.gitlab.com/api/projects/#get-a-single-project
func (c *Client) ProjectGet(
	ctx context.Context,
	project any,
	_ *GetProjectOptions,
) (*Project, *Response, error) {
	projectID, err := gitlabProjectResource(project)
	if err != nil {
		return nil, nil, err
	}

	var response Project
	resp, err := c.get(ctx, "projects/"+projectID, nil, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// UserCurrent fetches the authenticated GitLab user.
//
// GitLab API:
// https://docs.gitlab.com/api/users/#get-the-current-user
func (c *Client) UserCurrent(ctx context.Context) (*User, *Response, error) {
	var response User
	resp, err := c.get(ctx, "user", nil, &response)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// UserList lists GitLab users.
//
// GitLab API:
// https://docs.gitlab.com/api/users/#list-users
func (c *Client) UserList(
	ctx context.Context,
	opt *ListUsersOptions,
) ([]*User, *Response, error) {
	var response []*User
	resp, err := c.get(ctx, "users", opt.encodeQuery(), &response)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// MergeRequestCreate creates a merge request.
//
// GitLab API:
// https://docs.gitlab.com/api/merge_requests/#create-mr
func (c *Client) MergeRequestCreate(
	ctx context.Context,
	projectID int64,
	opt *CreateMergeRequestOptions,
) (*MergeRequest, *Response, error) {
	var response MergeRequest
	resp, err := c.post(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests", projectID),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeRequestGet fetches a single merge request.
//
// GitLab API:
// https://docs.gitlab.com/api/merge_requests/#get-single-mr
func (c *Client) MergeRequestGet(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	_ *GetMergeRequestsOptions,
) (*MergeRequest, *Response, error) {
	var response MergeRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests/%d", projectID, mergeRequest),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeRequestUpdate updates a merge request.
//
// GitLab API:
// https://docs.gitlab.com/api/merge_requests/#update-mr
func (c *Client) MergeRequestUpdate(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	opt *UpdateMergeRequestOptions,
) (*MergeRequest, *Response, error) {
	var response MergeRequest
	resp, err := c.put(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests/%d", projectID, mergeRequest),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeRequestList lists merge requests for a project.
//
// GitLab API:
// https://docs.gitlab.com/api/merge_requests/#list-project-merge-requests
func (c *Client) MergeRequestList(
	ctx context.Context,
	projectID int64,
	opt *ListProjectMergeRequestsOptions,
) ([]*BasicMergeRequest, *Response, error) {
	var response []*BasicMergeRequest
	resp, err := c.get(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests", projectID),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// MergeRequestAccept merges a merge request.
//
// GitLab API:
// https://docs.gitlab.com/api/merge_requests/#merge-a-merge-request
func (c *Client) MergeRequestAccept(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	opt *AcceptMergeRequestOptions,
) (*MergeRequest, *Response, error) {
	var response MergeRequest
	resp, err := c.put(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests/%d/merge", projectID, mergeRequest),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeRequestNoteCreate creates a merge request note.
//
// GitLab API:
// https://docs.gitlab.com/api/notes/#create-new-merge-request-note
func (c *Client) MergeRequestNoteCreate(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	opt *CreateMergeRequestNoteOptions,
) (*Note, *Response, error) {
	var response Note
	resp, err := c.post(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests/%d/notes", projectID, mergeRequest),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeRequestNoteUpdate updates a merge request note.
//
// GitLab API:
// https://docs.gitlab.com/api/notes/#modify-existing-merge-request-note
func (c *Client) MergeRequestNoteUpdate(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	noteID int64,
	opt *UpdateMergeRequestNoteOptions,
) (*Note, *Response, error) {
	var response Note
	resp, err := c.put(
		ctx,
		fmt.Sprintf(
			"projects/%d/merge_requests/%d/notes/%d",
			projectID,
			mergeRequest,
			noteID,
		),
		nil,
		opt,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// MergeRequestNoteList lists merge request notes.
//
// GitLab API:
// https://docs.gitlab.com/api/notes/#list-all-merge-request-notes
func (c *Client) MergeRequestNoteList(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	opt *ListMergeRequestNotesOptions,
) ([]*Note, *Response, error) {
	var response []*Note
	resp, err := c.get(
		ctx,
		fmt.Sprintf("projects/%d/merge_requests/%d/notes", projectID, mergeRequest),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// MergeRequestNoteDelete deletes a merge request note.
//
// GitLab API:
// https://docs.gitlab.com/api/notes/#delete-a-merge-request-note
func (c *Client) MergeRequestNoteDelete(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	noteID int64,
) (*Response, error) {
	return c.delete(
		ctx,
		fmt.Sprintf(
			"projects/%d/merge_requests/%d/notes/%d",
			projectID,
			mergeRequest,
			noteID,
		),
		nil,
	)
}

// MergeRequestDiscussionList lists merge request discussions.
//
// GitLab API:
// https://docs.gitlab.com/api/discussions/#list-project-merge-request-discussion-items
func (c *Client) MergeRequestDiscussionList(
	ctx context.Context,
	projectID int64,
	mergeRequest int64,
	opt *ListMergeRequestDiscussionsOptions,
) ([]*Discussion, *Response, error) {
	var response []*Discussion
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"projects/%d/merge_requests/%d/discussions",
			projectID,
			mergeRequest,
		),
		opt.encodeQuery(),
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// ProjectTemplateList lists project templates for a template type.
//
// GitLab API:
// https://docs.gitlab.com/api/project_templates/#get-all-templates-of-a-particular-type
func (c *Client) ProjectTemplateList(
	ctx context.Context,
	projectID int64,
	templateType string,
	_ *ListProjectTemplatesOptions,
) ([]*ProjectTemplate, *Response, error) {
	var response []*ProjectTemplate
	resp, err := c.get(
		ctx,
		fmt.Sprintf("projects/%d/templates/%s", projectID, templateType),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return response, resp, nil
}

// ProjectTemplateGet fetches a single project template.
//
// GitLab API:
// https://docs.gitlab.com/api/project_templates/#get-one-template-of-a-particular-type
func (c *Client) ProjectTemplateGet(
	ctx context.Context,
	projectID int64,
	templateType string,
	templateName string,
) (*ProjectTemplate, *Response, error) {
	var response ProjectTemplate
	resp, err := c.get(
		ctx,
		fmt.Sprintf(
			"projects/%d/templates/%s/%s",
			projectID,
			templateType,
			templateName,
		),
		nil,
		&response,
	)
	if err != nil {
		return nil, resp, err
	}
	return &response, resp, nil
}

// AccessLevelValue is GitLab's numeric project or group access level.
type AccessLevelValue int

const (
	// NoPermissions denies repository access.
	NoPermissions AccessLevelValue = 0
	// MinimalAccessPermissions grants minimal project access.
	MinimalAccessPermissions AccessLevelValue = 5
	// GuestPermissions grants guest project access.
	GuestPermissions AccessLevelValue = 10
	// PlannerPermissions grants planner project access.
	PlannerPermissions AccessLevelValue = 15
	// ReporterPermissions grants reporter project access.
	ReporterPermissions AccessLevelValue = 20
	// DeveloperPermissions grants developer project access.
	DeveloperPermissions AccessLevelValue = 30
	// MaintainerPermissions grants maintainer project access.
	MaintainerPermissions AccessLevelValue = 40
	// OwnerPermissions grants owner project access.
	OwnerPermissions AccessLevelValue = 50
	// AdminPermissions grants administrator project access.
	AdminPermissions AccessLevelValue = 60
)

// ListOptions configures GitLab's offset pagination.
type ListOptions struct {
	PerPage int64
	Page    int64
}

// LabelOptions serializes label lists
// in the comma-delimited form GitLab expects.
type LabelOptions []string

// MarshalJSON encodes labels
// as GitLab's comma-delimited string format.
func (l *LabelOptions) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("null"), nil
	}
	return json.Marshal(strings.Join(*l, ","))
}

// BasicUser matches the user shape
// embedded in other GitLab REST responses.
//
// GitLab users API:
// https://docs.gitlab.com/api/users/
type BasicUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// ProjectAccess matches the nested permissions shape
// returned from project lookups.
//
// GitLab projects API:
// https://docs.gitlab.com/api/projects/#get-a-single-project
type ProjectAccess struct {
	AccessLevel AccessLevelValue `json:"access_level"`
}

// GroupAccess matches the nested permissions shape
// returned from project lookups.
//
// GitLab projects API:
// https://docs.gitlab.com/api/projects/#get-a-single-project
type GroupAccess struct {
	AccessLevel AccessLevelValue `json:"access_level"`
}

// Permissions matches the subset of project permissions
// used by the forge.
//
// GitLab projects API:
// https://docs.gitlab.com/api/projects/#get-a-single-project
type Permissions struct {
	ProjectAccess *ProjectAccess `json:"project_access"`
	GroupAccess   *GroupAccess   `json:"group_access"`
}

// Project matches the subset of the project response the forge uses.
//
// GitLab projects API:
// https://docs.gitlab.com/api/projects/#get-a-single-project
type Project struct {
	ID          int64        `json:"id"`
	Permissions *Permissions `json:"permissions"`
}

// User matches the subset of the user response the forge uses.
//
// GitLab users API:
// https://docs.gitlab.com/api/users/#get-the-current-user
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// BasicMergeRequest matches the subset of merge request fields
// the forge reads from list and get endpoints.
//
// GitLab merge requests API:
// https://docs.gitlab.com/api/merge_requests/
type BasicMergeRequest struct {
	IID                     int64        `json:"iid"`
	SourceProjectID         int64        `json:"source_project_id"`
	TargetBranch            string       `json:"target_branch"`
	Title                   string       `json:"title"`
	State                   string       `json:"state"`
	Assignees               []*BasicUser `json:"assignees"`
	Reviewers               []*BasicUser `json:"reviewers"`
	Labels                  []string     `json:"labels"`
	Draft                   bool         `json:"draft"`
	SHA                     string       `json:"sha"`
	WebURL                  string       `json:"web_url"`
	ForceRemoveSourceBranch bool         `json:"force_remove_source_branch"`
}

// MergeRequest matches the merge request response shape
// used by the forge.
//
// GitLab merge requests API:
// https://docs.gitlab.com/api/merge_requests/
type MergeRequest struct {
	BasicMergeRequest
}

// NoteAuthor matches the nested author object in note responses.
//
// GitLab notes API:
// https://docs.gitlab.com/api/notes/
type NoteAuthor struct {
	ID int64 `json:"id"`
}

// Note matches the subset of note fields the forge uses.
//
// GitLab notes API:
// https://docs.gitlab.com/api/notes/
type Note struct {
	ID     int64      `json:"id"`
	Body   string     `json:"body"`
	Author NoteAuthor `json:"author"`
	System bool       `json:"system"`
}

// Discussion matches the subset of merge request discussion fields
// the forge uses.
//
// GitLab discussions API:
// https://docs.gitlab.com/api/discussions/
type Discussion struct {
	ID    string            `json:"id"`
	Notes []*DiscussionNote `json:"notes"`
}

// DiscussionNote matches the subset of discussion note fields
// the forge uses.
//
// GitLab discussions API:
// https://docs.gitlab.com/api/discussions/
type DiscussionNote struct {
	Resolvable bool `json:"resolvable"`
	Resolved   bool `json:"resolved"`
}

// ProjectTemplate matches the subset of project template fields
// the forge uses.
//
// GitLab project templates API:
// https://docs.gitlab.com/api/project_templates/
type ProjectTemplate struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// GetProjectOptions configures project lookup requests.
type GetProjectOptions struct{}

// GetMergeRequestsOptions configures merge-request lookup requests.
type GetMergeRequestsOptions struct{}

// ListUsersOptions configures GitLab user-list requests.
type ListUsersOptions struct {
	Username *string `json:"username,omitempty"`
}

func (o *ListUsersOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.Username != nil {
		values.Set("username", *o.Username)
	}
	return values
}

// CreateMergeRequestOptions configures merge-request creation.
type CreateMergeRequestOptions struct {
	Title              *string       `json:"title,omitempty"`
	Description        *string       `json:"description,omitempty"`
	SourceBranch       *string       `json:"source_branch,omitempty"`
	TargetBranch       *string       `json:"target_branch,omitempty"`
	TargetProjectID    *int64        `json:"target_project_id,omitempty"`
	Labels             *LabelOptions `json:"labels,omitempty"`
	AssigneeIDs        *[]int64      `json:"assignee_ids,omitempty"`
	ReviewerIDs        *[]int64      `json:"reviewer_ids,omitempty"`
	RemoveSourceBranch *bool         `json:"remove_source_branch,omitempty"`
}

// UpdateMergeRequestOptions configures merge-request updates.
type UpdateMergeRequestOptions struct {
	Title        *string       `json:"title,omitempty"`
	TargetBranch *string       `json:"target_branch,omitempty"`
	AssigneeIDs  *[]int64      `json:"assignee_ids,omitempty"`
	ReviewerIDs  *[]int64      `json:"reviewer_ids,omitempty"`
	AddLabels    *LabelOptions `json:"add_labels,omitempty"`
	StateEvent   *string       `json:"state_event,omitempty"`
}

// ListProjectMergeRequestsOptions configures project merge-request listing.
type ListProjectMergeRequestsOptions struct {
	ListOptions

	IIDs         *[]int64 `json:"iids,omitempty"`
	State        *string  `json:"state,omitempty"`
	OrderBy      *string  `json:"order_by,omitempty"`
	SourceBranch *string  `json:"source_branch,omitempty"`
}

func (o *ListProjectMergeRequestsOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}

	if o.IIDs != nil {
		for _, iid := range *o.IIDs {
			values.Add("iids[]", strconv.FormatInt(iid, 10))
		}
	}
	if o.State != nil {
		values.Set("state", *o.State)
	}
	if o.OrderBy != nil {
		values.Set("order_by", *o.OrderBy)
	}
	if o.SourceBranch != nil {
		values.Set("source_branch", *o.SourceBranch)
	}
	if o.PerPage != 0 {
		values.Set("per_page", strconv.FormatInt(o.PerPage, 10))
	}
	if o.Page != 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	return values
}

// AcceptMergeRequestOptions configures merge-request acceptance.
type AcceptMergeRequestOptions struct {
	ShouldRemoveSourceBranch *bool `json:"should_remove_source_branch,omitempty"`
}

// CreateMergeRequestNoteOptions configures note creation.
type CreateMergeRequestNoteOptions struct {
	Body *string `json:"body,omitempty"`
}

// UpdateMergeRequestNoteOptions configures note updates.
type UpdateMergeRequestNoteOptions struct {
	Body *string `json:"body,omitempty"`
}

// ListMergeRequestNotesOptions configures note-list requests.
type ListMergeRequestNotesOptions struct {
	ListOptions
	Sort *string `json:"sort,omitempty"`
}

func (o *ListMergeRequestNotesOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.Sort != nil {
		values.Set("sort", *o.Sort)
	}
	if o.PerPage != 0 {
		values.Set("per_page", strconv.FormatInt(o.PerPage, 10))
	}
	if o.Page != 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	return values
}

// ListMergeRequestDiscussionsOptions configures discussion-list requests.
type ListMergeRequestDiscussionsOptions struct {
	ListOptions
}

func (o *ListMergeRequestDiscussionsOptions) encodeQuery() url.Values {
	values := make(url.Values)
	if o == nil {
		return values
	}
	if o.PerPage != 0 {
		values.Set("per_page", strconv.FormatInt(o.PerPage, 10))
	}
	if o.Page != 0 {
		values.Set("page", strconv.FormatInt(o.Page, 10))
	}
	return values
}

// ListProjectTemplatesOptions configures template-list requests.
type ListProjectTemplatesOptions struct{}

func gitlabProjectResource(project any) (string, error) {
	switch v := project.(type) {
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case string:
		return strings.ReplaceAll(url.PathEscape(v), ".", "%2E"), nil
	default:
		return "", fmt.Errorf(
			"invalid GitLab project identifier type: %T",
			project,
		)
	}
}
