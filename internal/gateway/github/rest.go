package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

const (
	restAPIVersion = "2022-11-28"
	restMediaType  = "application/vnd.github+json"
)

// githubRESTError reports a non-successful GitHub REST response.
// Its diagnostic is bounded so error reporting cannot consume an unbounded
// response from GitHub or an intermediary.
type githubRESTError struct {
	statusCode int
	status     string
	diagnostic string
}

func (e *githubRESTError) Error() string {
	if e.diagnostic == "" {
		return "GitHub REST HTTP status " + e.status
	}
	return fmt.Sprintf("GitHub REST HTTP status %s: %s", e.status, e.diagnostic)
}

func (e *githubRESTError) Is(target error) bool {
	switch target {
	case ErrForbidden:
		return e.statusCode == http.StatusForbidden
	case ErrNotFound:
		return e.statusCode == http.StatusNotFound
	case ErrUnprocessable:
		return e.statusCode == http.StatusUnprocessableEntity
	default:
		return false
	}
}

// getREST performs an authenticated GET request and, when res is non-nil,
// decodes its JSON response into res.
func (c *Gateway) getREST(
	ctx context.Context,
	path []string,
	res any,
) error {
	return c.doREST(ctx, http.MethodGet, path, nil, res)
}

// postREST performs an authenticated POST request with req as its JSON body
// and, when res is non-nil, decodes the JSON response into res.
func (c *Gateway) postREST(
	ctx context.Context,
	path []string,
	req any,
	res any,
) error {
	return c.doREST(ctx, http.MethodPost, path, req, res)
}

// putREST performs an authenticated PUT request with req as its JSON body and,
// when res is non-nil, decodes the JSON response into res.
// acceptedStatuses identifies non-2xx responses whose bodies still use res's
// shape.
func (c *Gateway) putREST(
	ctx context.Context,
	path []string,
	req any,
	res any,
	acceptedStatuses ...int,
) error {
	return c.doREST(
		ctx,
		http.MethodPut,
		path,
		req,
		res,
		acceptedStatuses...,
	)
}

// doREST owns the transport protocol shared by typed REST operations.
// Every 2xx response is successful.
// The acceptedStatuses parameter adds operation-specific non-2xx responses
// whose bodies still use result's wire shape.
func (c *Gateway) doREST(
	ctx context.Context,
	method string,
	path []string,
	input any,
	result any,
	acceptedStatuses ...int,
) error {
	var requestBody io.Reader
	if input != nil {
		var body bytes.Buffer
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return fmt.Errorf("encode REST request: %w", err)
		}
		requestBody = &body
	}

	endpoint := c.restBaseURL.JoinPath(path...)
	req, err := c.newRequest(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", restMediaType)
	req.Header.Set("X-GitHub-Api-Version", restAPIVersion)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send REST request: %w", err)
	}
	if (res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices) &&
		!slices.Contains(acceptedStatuses, res.StatusCode) {
		return readRESTError(res)
	}

	responseBody, err := io.ReadAll(res.Body)
	err = errors.Join(err, res.Body.Close())
	if err != nil {
		return fmt.Errorf("read REST response: %w", err)
	}
	if result == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, result); err != nil {
		return fmt.Errorf("decode REST response: %w", err)
	}
	return nil
}

func readRESTError(res *http.Response) error {
	diagnostic, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	err = errors.Join(err, res.Body.Close())
	status := res.Status
	if status == "" {
		status = fmt.Sprintf("%d %s", res.StatusCode, http.StatusText(res.StatusCode))
	}
	restErr := &githubRESTError{
		statusCode: res.StatusCode,
		status:     status,
		diagnostic: strings.TrimSpace(string(diagnostic)),
	}
	if err == nil {
		return restErr
	}
	return errors.Join(restErr, fmt.Errorf("read REST response: %w", err))
}
