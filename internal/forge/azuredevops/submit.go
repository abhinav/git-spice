package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

// SubmitChange creates a new pull request in the repository.
func (r *Repository) SubmitChange(
	ctx context.Context,
	req forge.SubmitChangeRequest,
) (forge.SubmitChangeResult, error) {
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
		// Check if the error is due to the base branch not existing.
		// Azure DevOps returns a specific error for this.
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

	// Note: Labels, reviewers, and assignees are deferred to post-MVP.
	// They require additional API calls.

	return forge.SubmitChangeResult{
		ID:  &PR{Number: prID},
		URL: r.repoID.ChangeURL(&PR{Number: prID}),
	}, nil
}

func strPtr(s string) *string {
	return &s
}
