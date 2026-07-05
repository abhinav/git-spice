package forgejo

import (
	"context"
)

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
