package forgejo

import (
	"context"
)

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
