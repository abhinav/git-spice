package forgejo

import (
	"context"
)

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
