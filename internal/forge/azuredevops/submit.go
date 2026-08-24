package azuredevops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

const _unsupportedAssigneesWarning = "Azure DevOps does not support PR assignees; " +
	"ignoring --assign flags"

// SubmitChange creates a new pull request in the repository.
func (r *Repository) SubmitChange(
	ctx context.Context,
	req forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
	r.warnUnsupportedSubmitAssignees(req)

	// Azure DevOps requires full ref names.
	sourceRef := "refs/heads/" + req.Head
	targetRef := "refs/heads/" + req.Base

	createArgs := git.CreatePullRequestArgs{
		Project:      strPtr(r.project()),
		RepositoryId: strPtr(r.repositoryID()),
		GitPullRequestToCreate: &git.GitPullRequest{
			Title:         &req.Subject,
			Description:   &req.Body,
			SourceRefName: &sourceRef,
			TargetRefName: &targetRef,
			IsDraft:       &req.Draft,
		},
	}

	pr, err := r.client.gitClient.CreatePullRequest(ctx, createArgs)
	if err != nil {
		if exists, existsErr := r.refExists(ctx, req.Base); existsErr == nil && !exists {
			return forge.SubmitChangeResult{}, fmt.Errorf(
				"create pull request: %w",
				errors.Join(forge.ErrUnsubmittedBase, err),
			)
		}
		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	prID := 0
	if pr.PullRequestId != nil {
		prID = *pr.PullRequestId
	}

	r.log.Debug("Created pull request",
		"pr", prID,
		"url", r.repoID.ChangeURL(&PR{Number: prID}),
	)

	if err := r.addLabelsToPullRequest(ctx, prID, req.Labels); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add labels to pull request: %w", err)
	}

	if err := r.addReviewersToPullRequest(ctx, prID, req.Reviewers); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add reviewers to pull request: %w", err)
	}

	return forge.SubmitChangeResult{
		ID:  &PR{Number: prID},
		URL: r.repoID.ChangeURL(&PR{Number: prID}),
	}, nil
}

func (r *Repository) warnUnsupportedSubmitAssignees(req forge.SubmitChangeRequest) {
	if len(req.Assignees) > 0 {
		r.log.Warn(_unsupportedAssigneesWarning)
	}
}

func (r *Repository) refExists(ctx context.Context, branch string) (bool, error) {
	filter := "heads/" + strings.TrimPrefix(branch, "refs/heads/")
	top := 10
	refs, err := r.client.gitClient.GetRefs(ctx, git.GetRefsArgs{
		Project:      strPtr(r.project()),
		RepositoryId: strPtr(r.repositoryID()),
		Filter:       &filter,
		Top:          &top,
	})
	if err != nil {
		return false, fmt.Errorf("get refs: %w", err)
	}

	want := "refs/" + filter
	for _, ref := range refs.Value {
		if ref.Name != nil && *ref.Name == want {
			return true, nil
		}
	}
	return false, nil
}

func strPtr(s string) *string {
	return &s
}
