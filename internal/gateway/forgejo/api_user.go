package forgejo

import (
	"context"
	"net/url"
)

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
