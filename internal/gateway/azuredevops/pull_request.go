package azuredevops

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
)

// PullRequestStatus is the lifecycle state of an Azure DevOps pull request.
type PullRequestStatus int

const (
	// PullRequestStatusUnknown represents an unspecified or unrecognized state.
	PullRequestStatusUnknown PullRequestStatus = iota
	// PullRequestStatusActive represents an open pull request.
	PullRequestStatusActive
	// PullRequestStatusCompleted represents a merged pull request.
	PullRequestStatusCompleted
	// PullRequestStatusAbandoned represents a closed pull request.
	PullRequestStatusAbandoned
)

// MergeStatus is Azure DevOps' asynchronous merge evaluation state.
type MergeStatus int

const (
	// MergeStatusUnknown represents an unspecified or unrecognized state.
	MergeStatusUnknown MergeStatus = iota
	// MergeStatusSucceeded indicates that merge evaluation succeeded.
	MergeStatusSucceeded
	// MergeStatusConflicts indicates that merge conflicts block completion.
	MergeStatusConflicts
	// MergeStatusRejectedByPolicy indicates that repository policy blocks completion.
	MergeStatusRejectedByPolicy
	// MergeStatusFailure indicates that merge evaluation failed.
	MergeStatusFailure
	// MergeStatusQueued indicates that merge evaluation is pending.
	MergeStatusQueued
)

// PullRequest is an Azure DevOps pull request projected into the fields
// consumed by git-spice.
type PullRequest struct {
	// ID is the project-scoped pull request number.
	ID int

	// Title is the pull request title.
	Title string

	// Status is the pull request lifecycle state.
	Status PullRequestStatus

	// TargetRef is the full Git ref targeted by the pull request.
	TargetRef string

	// Draft reports whether the pull request is a draft.
	Draft bool

	// HeadCommit is the last source commit evaluated by Azure DevOps.
	HeadCommit string

	// HeadCommitURL is the API URL for HeadCommit.
	HeadCommitURL string

	// MergeStatus is the latest asynchronous merge evaluation state.
	MergeStatus MergeStatus

	// Labels contains label names when labels were included in the response.
	// A nil pointer distinguishes omitted labels from an included empty list.
	Labels *[]string

	// Reviewers contains reviewer names when reviewers were included in the
	// response.
	// A nil pointer distinguishes omitted reviewers from an included empty list.
	Reviewers *[]string
}

// PullRequest fetches one pull request.
// It returns [ErrNotFound] when Azure DevOps cannot find the pull request.
func (g *Gateway) PullRequest(
	ctx context.Context,
	project string,
	repository string,
	id int,
) (*PullRequest, error) {
	pr, err := g.gitClient.GetPullRequest(ctx, git.GetPullRequestArgs{
		Project:       &project,
		RepositoryId:  &repository,
		PullRequestId: &id,
	})
	if err != nil {
		return nil, normalizeError(err)
	}
	return pullRequestFromSDK(pr), nil
}

func pullRequestFromSDK(pr *git.GitPullRequest) *PullRequest {
	if pr == nil {
		return nil
	}
	result := &PullRequest{}
	if pr.PullRequestId != nil {
		result.ID = *pr.PullRequestId
	}
	if pr.Title != nil {
		result.Title = *pr.Title
	}
	if pr.Status != nil {
		result.Status = pullRequestStatusFromSDK(*pr.Status)
	}
	if pr.TargetRefName != nil {
		result.TargetRef = *pr.TargetRefName
	}
	if pr.IsDraft != nil {
		result.Draft = *pr.IsDraft
	}
	if pr.LastMergeSourceCommit != nil && pr.LastMergeSourceCommit.CommitId != nil {
		result.HeadCommit = *pr.LastMergeSourceCommit.CommitId
	}
	if pr.LastMergeSourceCommit != nil && pr.LastMergeSourceCommit.Url != nil {
		result.HeadCommitURL = *pr.LastMergeSourceCommit.Url
	}
	if pr.MergeStatus != nil {
		result.MergeStatus = mergeStatusFromSDK(*pr.MergeStatus)
	}
	if pr.Labels != nil {
		labels := labelsFromSDK(pr.Labels)
		result.Labels = &labels
	}
	if pr.Reviewers != nil {
		reviewers := reviewersFromSDK(pr.Reviewers)
		result.Reviewers = &reviewers
	}
	return result
}

func pullRequestStatusFromSDK(status git.PullRequestStatus) PullRequestStatus {
	switch status {
	case git.PullRequestStatusValues.Active:
		return PullRequestStatusActive
	case git.PullRequestStatusValues.Completed:
		return PullRequestStatusCompleted
	case git.PullRequestStatusValues.Abandoned:
		return PullRequestStatusAbandoned
	default:
		return PullRequestStatusUnknown
	}
}

func pullRequestStatusToSDK(status PullRequestStatus) git.PullRequestStatus {
	switch status {
	case PullRequestStatusActive:
		return git.PullRequestStatusValues.Active
	case PullRequestStatusCompleted:
		return git.PullRequestStatusValues.Completed
	case PullRequestStatusAbandoned:
		return git.PullRequestStatusValues.Abandoned
	default:
		return git.PullRequestStatusValues.All
	}
}

func mergeStatusFromSDK(status git.PullRequestAsyncStatus) MergeStatus {
	switch status {
	case git.PullRequestAsyncStatusValues.Succeeded:
		return MergeStatusSucceeded
	case git.PullRequestAsyncStatusValues.Conflicts:
		return MergeStatusConflicts
	case git.PullRequestAsyncStatusValues.RejectedByPolicy:
		return MergeStatusRejectedByPolicy
	case git.PullRequestAsyncStatusValues.Failure:
		return MergeStatusFailure
	case git.PullRequestAsyncStatusValues.Queued:
		return MergeStatusQueued
	default:
		return MergeStatusUnknown
	}
}

func labelsFromSDK(labels *[]core.WebApiTagDefinition) []string {
	var result []string
	for _, label := range *labels {
		if label.Name != nil {
			result = append(result, *label.Name)
		}
	}
	return result
}

func reviewersFromSDK(reviewers *[]git.IdentityRefWithVote) []string {
	var result []string
	for _, reviewer := range *reviewers {
		switch {
		case reviewer.UniqueName != nil:
			result = append(result, *reviewer.UniqueName)
		case reviewer.DisplayName != nil:
			result = append(result, *reviewer.DisplayName)
		case reviewer.Id != nil:
			result = append(result, *reviewer.Id)
		}
	}
	return result
}
