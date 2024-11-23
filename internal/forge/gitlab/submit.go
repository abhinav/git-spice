package gitlab

import (
	"context"
	"fmt"

	"github.com/xanzy/go-gitlab"
	"go.abhg.dev/gs/internal/forge"
)

// DRAFT is the prefix for draft merge requests.
const DRAFT = "Draft:"

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
		input.Title = gitlab.Ptr(fmt.Sprintf("%s %s", DRAFT, req.Subject))
	}
	request, _, err := r.mergeRequests.CreateMergeRequest(
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
