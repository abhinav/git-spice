package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/graphqlutil"
)

func newGitHubv4EnterpriseClient(
	url string,
	httpClient *http.Client,
) *githubv4.Client {
	httpClient.Transport = graphqlutil.WrapTransport(httpClient.Transport)
	return githubv4.NewEnterpriseClient(url, httpClient)
}

// githubv3Client implements a minimal GitHub v3 client
// to prevent use of the go-github library directly.
// go-github's Client type bundles all services into one massive type,
// which inflates the binary size a couple megabytes.
// Normally, those would be dropped by the linker if unused,
// but because we use reflect.MethodByName (through Kong at least),
// the linker cannot determine what methods are used and what are not.
//
// So for now we implement our own minimal client
// that only implements the methods we actually need.
type githubv3Client struct {
	client *http.Client
	apiURL *url.URL
}

// newGitHubv3Client creates a new GitHub v3 client.
// If apiURL is nil, the default API URL for GitHub is used.
func newGitHubv3Client(client *http.Client, apiURL *url.URL) *githubv3Client {
	if apiURL == nil {
		var err error
		apiURL, err = url.Parse(DefaultAPIURL)
		if err != nil {
			panic("invalid default GitHub API URL")
		}
	}

	return &githubv3Client{
		client: client,
		apiURL: apiURL,
	}
}

type githubv3Response struct {
	// Response metadata can be added here as needed.
	// Right now, we don't need any pagination or rate limit info.
}

// githubv3ResponseError represents an error returned by the GitHub REST API v3.
type githubv3ResponseError struct {
	// StatusCode is the HTTP status code of the response.
	// This is not part of the JSON response body.
	StatusCode int `json:"-"`

	// Message is a short description of the error.
	Message string `json:"message"`

	// Errors contains detailed error information, if any.
	Errors []*githubv3Error `json:"errors"`

	// DocumentationURL is a URL to the relevant API documentation.
	// Most error responses include this field.
	DocumentationURL string `json:"documentation_url,omitempty"`
}

func (e *githubv3ResponseError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "GitHub API error (status %d): %s", e.StatusCode, e.Message)

	for _, err := range e.Errors {
		fmt.Fprintf(&sb, "\n  - %v", err)
	}

	if e.DocumentationURL != "" {
		fmt.Fprintf(&sb, "\nFor more information, see: %s", e.DocumentationURL)
	}

	return sb.String()
}

// githubv3Error is part of a GitHub v3 error response.
// Sometimes these can be just a string,
// so we need to implement custom unmarshaling.
type githubv3Error struct {
	Resource string `json:"resource"` // resource on which error occurred
	Field    string `json:"field"`    // field on which error occurred
	Code     string `json:"code"`     // validation error code
	Message  string `json:"message"`  // error message (always set for "custom" code)
}

func (e *githubv3Error) UnmarshalJSON(data []byte) error {
	// Try plain string error first.
	var msg string
	if err := json.Unmarshal(data, &msg); err == nil {
		e.Message = msg
		return nil
	}

	// Otherwise, try as structured error.
	type rawError githubv3Error
	return json.Unmarshal(data, (*rawError)(e))
}

func (e *githubv3Error) Error() string {
	var sb strings.Builder

	// Put the resource and field as:
	//
	//   resource
	//   resource.field
	//   field
	//
	// depending on what is available.
	sb.WriteString(e.Resource)
	if e.Field != "" {
		if sb.Len() > 0 {
			sb.WriteString(".")
		}
		sb.WriteString(e.Field)
	}
	if sb.Len() > 0 {
		sb.WriteString(": ")
	}

	// resource.field: message
	// resource.field: code (message)
	if e.Code == "custom" || e.Code == "" {
		sb.WriteString(e.Message)
	} else {
		sb.WriteString(e.Code)
		if e.Message != "" {
			fmt.Fprintf(&sb, " (%s)", e.Message)
		}
	}

	return sb.String()
}

// Do performs a generic HTTP request using the GitHub v3 client.
//
// reqBody is the JSON object to send as the request body (or nil for no body).
// resBody is a pointer to the object to decode the JSON response into
// (or nil to ignore the response body).
func (c *githubv3Client) Do(
	ctx context.Context,
	method, path string,
	reqBody, resBody any,
) (*githubv3Response, error) {
	var body io.Reader
	if reqBody != nil {
		bs, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		body = bytes.NewReader(bs)
	}

	req, err := http.NewRequestWithContext(
		ctx, method, c.apiURL.JoinPath(path).String(), body,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "X-GitHub-Api-Version: 2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if resBody != nil {
		req.Header.Set("Accept", "application/vnd.github+json")
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	// Non-200 responses are considered errors.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		bs, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("read error response: %w", err)
		}

		// Try to unmarshal the full error response.
		// If this fails, fall back to using raw body as message.
		ghErr := &githubv3ResponseError{StatusCode: res.StatusCode}
		if err := json.Unmarshal(bs, ghErr); err != nil {
			ghErr.Message = string(bs)
		}
		return nil, ghErr
	}

	if res.StatusCode == http.StatusNoContent {
		return &githubv3Response{}, nil
	}

	if resBody != nil {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}

		if err := json.Unmarshal(body, resBody); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
	} else {
		// Drain the response body even if we don't care about it,
		// so the underlying HTTP connection can be reused.
		_, _ = io.Copy(io.Discard, res.Body)
	}

	return &githubv3Response{}, nil
}

// ReviewersRequest specifies users and teams for a pull request review request.
type ReviewersRequest struct {
	Reviewers     []string `json:"reviewers,omitempty"`
	TeamReviewers []string `json:"team_reviewers,omitempty"`
}

// PullRequestRequestReviewers requests reviews from users and teams
// on a pull request.
//
// Required permissions:
// Pull requests" repository permissions (write).
//
// Ref: https://docs.github.com/en/rest/pulls/review-requests?apiVersion=2022-11-28#request-reviewers-for-a-pull-request
func (c *githubv3Client) PullRequestRequestReviewers(
	ctx context.Context,
	owner, repo string,
	number int,
	reviewers *ReviewersRequest,
) error {
	u := fmt.Sprintf("repos/%v/%v/pulls/%v/requested_reviewers", owner, repo, number)
	_, err := c.Do(ctx, "POST", u, reviewers, new(any))
	return err
}
