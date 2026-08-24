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

	r.warnUnsupportedEditAssignees(opts)

	if hasUpdate {
		_, err := r.client.gitClient.UpdatePullRequest(ctx, git.UpdatePullRequestArgs{
			Project:                new(r.project()),
			RepositoryId:           new(r.repositoryID()),
			PullRequestId:          &prID,
			GitPullRequestToUpdate: update,
		})
		if err != nil {
			return fmt.Errorf("update pull request: %w", err)
		}

		r.log.Debug("Updated pull request", "pr", prID)
	}

	if err := r.addLabelsToPullRequest(ctx, prID, opts.AddLabels); err != nil {
		return fmt.Errorf("add labels to pull request: %w", err)
	}

	if err := r.addReviewersToPullRequest(ctx, prID, opts.AddReviewers); err != nil {
		return fmt.Errorf("add reviewers to pull request: %w", err)
	}

	return nil
}

func (r *Repository) warnUnsupportedEditAssignees(opts forge.EditChangeOptions) {
	if len(opts.AddAssignees) > 0 {
		r.log.Warn(_unsupportedAssigneesWarning)
	}
}
