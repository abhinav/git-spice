package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

// EditChange updates an existing pull request.
func (r *Repository) EditChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.EditChangeOptions,
) error {
	prID := mustPR(id).Number

	// Build the update payload.
	update := &git.GitPullRequest{}
	hasUpdate := false

	// Update target (base) branch.
	if opts.Base != "" {
		targetRef := "refs/heads/" + opts.Base
		update.TargetRefName = &targetRef
		hasUpdate = true
	}

	// Update draft status.
	if opts.Draft != nil {
		update.IsDraft = opts.Draft
		hasUpdate = true
	}

	if !hasUpdate {
		// Nothing to update.
		return nil
	}

	_, err := r.client.gitClient.UpdatePullRequest(ctx, git.UpdatePullRequestArgs{
		Project:                strPtr(r.project()),
		RepositoryId:           strPtr(r.repositoryID()),
		PullRequestId:          &prID,
		GitPullRequestToUpdate: update,
	})
	if err != nil {
		return fmt.Errorf("update pull request: %w", err)
	}

	r.log.Debug("Updated pull request", "pr", prID)

	// Note: Labels, reviewers, and assignees are deferred to post-MVP.
	// They require separate API calls to the labels/reviewers endpoints.

	return nil
}
