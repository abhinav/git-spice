package azuredevops

import (
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
)

func TestPullRequestFromSDK(t *testing.T) {
	labels := []core.WebApiTagDefinition{
		{Name: new("stack")},
		{},
	}
	reviewers := []git.IdentityRefWithVote{
		{UniqueName: new("octo@example.com")},
		{DisplayName: new("Mona Lisa")},
		{Id: new("identity-id")},
	}
	status := git.PullRequestStatusValues.Active
	mergeStatus := git.PullRequestAsyncStatusValues.Queued

	got := pullRequestFromSDK(&git.GitPullRequest{
		PullRequestId: new(42),
		Title:         new("Stacked change"),
		Status:        &status,
		TargetRefName: new("refs/heads/main"),
		IsDraft:       new(true),
		LastMergeSourceCommit: &git.GitCommitRef{
			CommitId: new("abcdef"),
			Url:      new("https://example.test/commits/abcdef"),
		},
		MergeStatus: &mergeStatus,
		Labels:      &labels,
		Reviewers:   &reviewers,
	})

	assert.Equal(t, &PullRequest{
		ID:            42,
		Title:         "Stacked change",
		Status:        PullRequestStatusActive,
		TargetRef:     "refs/heads/main",
		Draft:         true,
		HeadCommit:    "abcdef",
		HeadCommitURL: "https://example.test/commits/abcdef",
		MergeStatus:   MergeStatusQueued,
		Labels:        &[]string{"stack"},
		Reviewers: &[]string{
			"octo@example.com",
			"Mona Lisa",
			"identity-id",
		},
	}, got)
}

func TestPullRequestFromSDK_omittedCollections(t *testing.T) {
	got := pullRequestFromSDK(&git.GitPullRequest{})

	assert.Nil(t, got.Labels)
	assert.Nil(t, got.Reviewers)
}

func TestMergeMethodToSDK_default(t *testing.T) {
	assert.Nil(t, mergeMethodToSDK(MergeMethodDefault))
}
