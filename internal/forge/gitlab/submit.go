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
	if r.removeSourceBranchOnMerge {
		input.RemoveSourceBranch = gitlab.Ptr(true)
	}
	if req.Body != "" {
		input.Description = &req.Body
	}
	if req.Draft {
		input.Title = gitlab.Ptr(fmt.Sprintf("%s %s", _draftPrefix, req.Subject))
	}
	if len(req.Labels) > 0 {
		input.Labels = (*gitlab.LabelOptions)(&req.Labels)
	}

	if len(req.Reviewers) > 0 {
		reviewerIDs, err := r.resolveReviewerIDs(ctx, req.Reviewers)
		if err != nil {
			return forge.SubmitChangeResult{}, fmt.Errorf("resolve reviewer IDs: %w", err)
		}
		input.ReviewerIDs = &reviewerIDs
	}

	if len(req.Assignees) > 0 {
		assigneeIDs, err := r.assigneeIDs(ctx, req.Assignees)
		if err != nil {
			return forge.SubmitChangeResult{}, fmt.Errorf("resolve assignees: %w", err)
		}
		input.AssigneeIDs = &assigneeIDs
	}

	request, _, err := r.client.MergeRequests.CreateMergeRequest(
		r.repoID, input,
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("create merge request: %w", err)
	}
	r.log.Debug("Created merge request",
		"mr", request.IID,
		"url", request.WebURL)

	return forge.SubmitChangeResult{
		ID: &MR{
			Number: request.IID,
		},
		URL: request.WebURL,
	}, nil
}
