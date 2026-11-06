package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// mergeAsync submits a prepared merge request.
// A nil operation means the response was already merged or enqueued;
// the caller still observes change states to determine final completion.
func (r *Repository) mergeAsync(
	ctx context.Context,
	req *github.MergePullRequestAsyncInput,
) (forge.MergeOperation, error) {
	result, err := r.gateway.MergePullRequestAsync(ctx, req)
	if errors.Is(err, github.ErrNotFound) {
		return nil, fmt.Errorf(
			"submit GitHub asynchronous merge: %w",
			errors.Join(forge.ErrUnsupported, err),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("submit GitHub asynchronous merge: %w", err)
	}
	status, err := githubMergeOperationStatus(result)
	if err != nil {
		return nil, err
	}
	if status == forge.MergeOperationPending {
		return &githubMergeOperation{
			repository:  r,
			pullRequest: req.PullRequestNumber,
			operationID: result.OperationID,
		}, nil
	}
	return nil, nil
}

// githubMergeOperation retains the identity of a pending asynchronous request
// so later status probes observe that same request rather than submitting again.
type githubMergeOperation struct {
	repository  *Repository // required
	pullRequest int
	operationID string
}

var _ forge.MergeOperation = (*githubMergeOperation)(nil)

// Status performs one GitHub asynchronous merge status probe.
func (o *githubMergeOperation) Status(
	ctx context.Context,
) (forge.MergeOperationStatus, error) {
	result, err := o.repository.gateway.AsyncMergeResult(
		ctx,
		o.repository.owner,
		o.repository.repo,
		o.pullRequest,
		o.operationID,
	)
	if err != nil {
		return 0, fmt.Errorf("poll GitHub asynchronous merge: %w", err)
	}
	return githubMergeOperationStatus(result)
}

// githubMergeOperationStatus is the forge-facing interpretation shared by
// initial submission and later status probes. Both a completed merge and a
// merge-queue admission release the caller to observe pull request state;
// neither requires another asynchronous-operation probe.
func githubMergeOperationStatus(
	result *github.AsyncMergeResult,
) (forge.MergeOperationStatus, error) {
	switch result.Status {
	case github.AsyncMergeStatusPending:
		return forge.MergeOperationPending, nil
	case github.AsyncMergeStatusMerged, github.AsyncMergeStatusEnqueued:
		return forge.MergeOperationAccepted, nil
	}
	message := result.Message
	if strings.TrimSpace(message) == "" {
		message = "GitHub did not provide a failure reason"
	}
	return 0, fmt.Errorf("GitHub asynchronous merge failed: %s", message)
}
