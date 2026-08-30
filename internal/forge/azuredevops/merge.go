package azuredevops

import (
	"context"
	"fmt"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"go.abhg.dev/gs/internal/forge"
)

// MergeChange merges an open pull request into its base branch.
func (r *Repository) MergeChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	prID := mustPR(id).Number

	completionOptions := &git.GitPullRequestCompletionOptions{
		MergeStrategy: mergeStrategy(opts.Method),
	}

	update := &git.GitPullRequest{
		Status:            &git.PullRequestStatusValues.Completed,
		CompletionOptions: completionOptions,
	}
	if opts.HeadHash != "" {
		commitID := opts.HeadHash.String()
		update.LastMergeSourceCommit = &git.GitCommitRef{
			CommitId: &commitID,
		}
	}

	if _, err := r.client.gitClient.UpdatePullRequest(ctx, git.UpdatePullRequestArgs{
		Project:                new(r.project()),
		RepositoryId:           new(r.repositoryID()),
		PullRequestId:          &prID,
		GitPullRequestToUpdate: update,
	}); err != nil {
		return fmt.Errorf("complete pull request: %w", err)
	}

	r.log.Debug("Merged pull request", "pr", prID)
	return nil
}

func mergeStrategy(method forge.MergeMethod) *git.GitPullRequestMergeStrategy {
	switch method {
	case forge.MergeMethodDefault:
		return nil
	case forge.MergeMethodMerge:
		return &git.GitPullRequestMergeStrategyValues.NoFastForward
	case forge.MergeMethodSquash:
		return &git.GitPullRequestMergeStrategyValues.Squash
	case forge.MergeMethodRebase:
		return &git.GitPullRequestMergeStrategyValues.Rebase
	default:
		return nil
	}
}
