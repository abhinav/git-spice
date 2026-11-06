package azuredevops

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
)

// MergeChange merges an open pull request into its base branch.
func (r *Repository) MergeChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	prID := mustPR(id).Number

	status := azuredevops.PullRequestStatusCompleted
	update := &azuredevops.UpdatePullRequestInput{
		Project:    r.project(),
		Repository: r.repositoryID(),
		ID:         prID,
		Status:     &status,
		Completion: &azuredevops.CompletionOptions{
			MergeMethod: mergeStrategy(opts.Method),
		},
	}
	if opts.HeadHash != "" {
		update.HeadCommit = opts.HeadHash.String()
	}

	if err := r.gateway.UpdatePullRequest(ctx, update); err != nil {
		return fmt.Errorf("complete pull request: %w", err)
	}

	r.log.Debug("Merged pull request", "pr", prID)
	return nil
}

func mergeStrategy(method forge.MergeMethod) azuredevops.MergeMethod {
	switch method {
	case forge.MergeMethodDefault:
		return azuredevops.MergeMethodDefault
	case forge.MergeMethodMerge:
		return azuredevops.MergeMethodNoFastForward
	case forge.MergeMethodSquash:
		return azuredevops.MergeMethodSquash
	case forge.MergeMethodRebase:
		return azuredevops.MergeMethodRebase
	default:
		return azuredevops.MergeMethodDefault
	}
}
