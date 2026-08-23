package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// MergePullRequestAsyncInput specifies one asynchronous pull request merge.
type MergePullRequestAsyncInput struct {
	// Owner is the login that owns the repository.
	Owner string // required

	// Repo is the repository name.
	Repo string // required

	// PullRequestNumber identifies the pull request to merge.
	// When the pull request belongs to a stack, GitHub merges the open stack
	// prefix ending at this pull request.
	PullRequestNumber int // required

	// ExpectedHeadSHA, when non-empty, requires the pull request head to match
	// before merging.
	ExpectedHeadSHA string

	// Method selects the merge strategy.
	// The unknown value lets GitHub select the repository default.
	Method MergeMethod
}

// AsyncMergeResult describes the current state of an asynchronous merge.
type AsyncMergeResult struct {
	// Status is GitHub's current disposition of the request.
	Status AsyncMergeStatus

	// Message is GitHub's human-readable description, when provided.
	Message string

	// OperationID identifies a pending operation for later status probes.
	// It is non-empty when Status is [AsyncMergeStatusPending].
	OperationID string
}

// MergePullRequestAsync submits one asynchronous pull request merge.
// GitHub's failed and already-pending responses are returned as results because
// those responses use the same protocol as successful submissions.
// See https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously.
func (c *Gateway) MergePullRequestAsync(
	ctx context.Context,
	input *MergePullRequestAsyncInput,
) (*AsyncMergeResult, error) {
	var mergeMethod string
	if input.Method != MergeMethodUnknown {
		method, err := input.Method.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("encode merge method: %w", err)
		}
		mergeMethod = strings.ToLower(string(method))
	}
	req := struct {
		ExpectedHeadSHA string `json:"sha,omitempty"`
		MergeMethod     string `json:"merge_method,omitempty"`
		MergeAction     string `json:"merge_action"`
	}{
		ExpectedHeadSHA: input.ExpectedHeadSHA,
		MergeMethod:     mergeMethod,
		MergeAction:     "default",
	}

	path := []string{
		"repos",
		input.Owner,
		input.Repo,
		"pulls",
		strconv.Itoa(input.PullRequestNumber),
		"merge-async",
	}
	var res struct {
		Status  AsyncMergeStatus `json:"status"`
		Details struct {
			Message string `json:"message"`
			UUID    string `json:"uuid"`
		} `json:"details"`
	}
	err := c.putREST(
		ctx,
		path,
		&req,
		&res,
		http.StatusBadRequest,
		http.StatusConflict,
	)
	if err != nil {
		return nil, fmt.Errorf("submit asynchronous merge: %w", err)
	}
	result := &AsyncMergeResult{
		Status:      res.Status,
		Message:     res.Details.Message,
		OperationID: res.Details.UUID,
	}
	if result.Status == AsyncMergeStatusPending && result.OperationID == "" {
		return nil, errors.New("pending asynchronous merge has no operation ID")
	}
	return result, nil
}

// AsyncMergeResult fetches the current state of one asynchronous merge.
// It performs one request and does not poll or wait.
// See https://docs.github.com/en/rest/pulls/pulls#get-the-result-of-an-asynchronous-merge.
func (c *Gateway) AsyncMergeResult(
	ctx context.Context,
	owner string,
	repo string,
	pullRequestNumber int,
	operationID string,
) (*AsyncMergeResult, error) {
	var res struct {
		Status  AsyncMergeStatus `json:"status"`
		Details struct {
			Message string `json:"message"`
			UUID    string `json:"uuid"`
		} `json:"details"`
	}
	path := []string{
		"repos",
		owner,
		repo,
		"pulls",
		strconv.Itoa(pullRequestNumber),
		"merge-async",
		operationID,
	}
	if err := c.getREST(ctx, path, &res); err != nil {
		return nil, fmt.Errorf("get asynchronous merge result: %w", err)
	}

	result := &AsyncMergeResult{
		Status:      res.Status,
		Message:     res.Details.Message,
		OperationID: res.Details.UUID,
	}
	if result.Status == AsyncMergeStatusPending && result.OperationID == "" {
		return nil, errors.New("pending asynchronous merge has no operation ID")
	}
	return result, nil
}

// AsyncMergeStatus is GitHub's disposition of one asynchronous merge request.
type AsyncMergeStatus int

const (
	// AsyncMergeStatusUnknown is the zero value and is not returned by GitHub.
	AsyncMergeStatusUnknown AsyncMergeStatus = iota

	// AsyncMergeStatusPending means GitHub is still processing the request.
	AsyncMergeStatusPending

	// AsyncMergeStatusMerged means GitHub completed the merge.
	AsyncMergeStatusMerged

	// AsyncMergeStatusEnqueued means GitHub accepted the request into a merge
	// queue.
	// Callers must observe pull request state for final completion.
	AsyncMergeStatusEnqueued

	// AsyncMergeStatusFailed means GitHub rejected the merge request.
	AsyncMergeStatusFailed
)

// UnmarshalJSON decodes the status strings returned by the async merge API.
func (s *AsyncMergeStatus) UnmarshalJSON(data []byte) error {
	var status string
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("decode asynchronous merge status: %w", err)
	}
	switch status {
	case "pending":
		*s = AsyncMergeStatusPending
	case "merged":
		*s = AsyncMergeStatusMerged
	case "enqueued":
		*s = AsyncMergeStatusEnqueued
	case "failed":
		*s = AsyncMergeStatusFailed
	default:
		return fmt.Errorf("unknown asynchronous merge status %q", status)
	}
	return nil
}
