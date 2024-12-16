package gitlab

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.abhg.dev/gs/internal/forge"
)

// _draftPrefix is the prefix added for Draft merge requests.
// (GitLab identifies draft MRs by their title.)
const _draftPrefix = "Draft:"

// SubmitChange creates a new change in a repository.
func (r *Repository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	input := &gitlab.CreateMergeRequestOptions{
		Title:        &req.Subject,
		TargetBranch: &req.Base,
		SourceBranch: &req.Head,
	}
	if req.Body != "" {
		input.Description = &req.Body
	}
	if req.Draft {
		input.Title = gitlab.Ptr(fmt.Sprintf("%s %s", _draftPrefix, req.Subject))
	}
	request, _, err := r.client.MergeRequests.CreateMergeRequest(
		r.repoID, input,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("create merge request: %w", err)
	}

	return forge.SubmitChangeResult{
		ID: &MR{
			Number: request.IID,
		},
		URL: request.WebURL,
	}, nil
}
