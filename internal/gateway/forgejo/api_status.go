package forgejo

import (
	"context"
	"fmt"
	"net/url"
)

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
