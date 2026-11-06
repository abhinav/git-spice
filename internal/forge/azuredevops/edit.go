package azuredevops

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
)

// EditChange updates an existing pull request.
func (r *Repository) EditChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.EditChangeOptions,
) error {
	prID := mustPR(id).Number

	// Build the update payload.
	update := &azuredevops.UpdatePullRequestInput{
		Project:    r.project(),
		Repository: r.repositoryID(),
		ID:         prID,
	}
	hasUpdate := false

	// Update target (base) branch.
	if opts.Base != "" {
		targetRef := "refs/heads/" + opts.Base
		update.TargetRef = &targetRef
		hasUpdate = true
	}

	// Update draft status.
	if opts.Draft != nil {
		update.Draft = opts.Draft
		hasUpdate = true
	}

	r.warnUnsupportedEditAssignees(opts)

	if hasUpdate {
		err := r.gateway.UpdatePullRequest(ctx, update)
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
