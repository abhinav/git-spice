package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// UpdatePullRequestInput describes pull request fields to change.
// Pointer fields are omitted from the Azure DevOps update when nil.
type UpdatePullRequestInput struct {
	// Project identifies the Azure DevOps project containing Repository.
	Project string

	// Repository identifies the repository by name or UUID.
	Repository string

	// ID is the project-scoped pull request number.
	ID int

	// TargetRef changes the full Git target ref when non-nil.
	TargetRef *string

	// Draft changes draft state when non-nil.
	Draft *bool

	// Status changes lifecycle state when non-nil.
	Status *PullRequestStatus

	// HeadCommit is the source commit Azure DevOps must merge.
	// An empty value leaves the source commit unspecified.
	HeadCommit string

	// HeadCommitURL is the Azure DevOps API URL for HeadCommit.
	HeadCommitURL string

	// Completion requests pull request completion when non-nil.
	Completion *CompletionOptions
}

// CompletionOptions configures pull request completion.
type CompletionOptions struct {
	// MergeMethod selects the commit strategy.
	// MergeMethodDefault leaves the strategy unspecified for Azure DevOps.
	MergeMethod MergeMethod

	// DeleteSource controls source branch deletion when non-nil.
	DeleteSource *bool
}

// MergeMethod selects Azure DevOps' completion strategy.
type MergeMethod int

const (
	// MergeMethodDefault leaves the strategy unspecified for Azure DevOps.
	MergeMethodDefault MergeMethod = iota
	// MergeMethodNoFastForward creates a merge commit.
	MergeMethodNoFastForward
	// MergeMethodSquash squashes the source commits.
	MergeMethodSquash
	// MergeMethodRebase rebases the source commits.
	MergeMethodRebase
)

// UpdatePullRequest changes the supplied fields or completes a pull request.
func (g *Gateway) UpdatePullRequest(
	ctx context.Context,
	in *UpdatePullRequestInput,
) error {
	request := &git.GitPullRequest{
		TargetRefName: in.TargetRef,
		IsDraft:       in.Draft,
	}
	if in.Status != nil {
		status := pullRequestStatusToSDK(*in.Status)
		request.Status = &status
	}
	if in.HeadCommit != "" {
		request.LastMergeSourceCommit = &git.GitCommitRef{
			CommitId: &in.HeadCommit,
			Url:      &in.HeadCommitURL,
		}
	}
	if in.Completion != nil {
		request.CompletionOptions = &git.GitPullRequestCompletionOptions{
			DeleteSourceBranch: in.Completion.DeleteSource,
			MergeStrategy:      mergeMethodToSDK(in.Completion.MergeMethod),
		}
	}

	_, err := g.gitClient.UpdatePullRequest(ctx, git.UpdatePullRequestArgs{
		Project:                &in.Project,
		RepositoryId:           &in.Repository,
		PullRequestId:          &in.ID,
		GitPullRequestToUpdate: request,
	})
	return normalizeError(err)
}

func mergeMethodToSDK(method MergeMethod) *git.GitPullRequestMergeStrategy {
	switch method {
	case MergeMethodNoFastForward:
		return &git.GitPullRequestMergeStrategyValues.NoFastForward
	case MergeMethodSquash:
		return &git.GitPullRequestMergeStrategyValues.Squash
	case MergeMethodRebase:
		return &git.GitPullRequestMergeStrategyValues.Rebase
	default:
		return nil
	}
}
