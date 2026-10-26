package forgejo

import (
	"context"
	"net/url"
)

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
