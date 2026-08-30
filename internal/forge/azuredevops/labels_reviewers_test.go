package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.uber.org/mock/gomock"
)

func TestRepository_SubmitChange_addsLabelsAndReviewers(t *testing.T) {
	var gotLabels []string
	var gotReviewers []string
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().CreatePullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ git.CreatePullRequestArgs,
		) (*git.GitPullRequest, error) {
			prID := 42
			return &git.GitPullRequest{PullRequestId: &prID}, nil
		},
	)
	client.EXPECT().CreatePullRequestLabel(gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(
			_ context.Context,
			args git.CreatePullRequestLabelArgs,
		) (*core.WebApiTagDefinition, error) {
			gotLabels = append(gotLabels, *args.Label.Name)
			return &core.WebApiTagDefinition{Name: args.Label.Name}, nil
		},
	)
	client.EXPECT().CreateUnmaterializedPullRequestReviewer(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			args git.CreateUnmaterializedPullRequestReviewerArgs,
		) (*git.IdentityRefWithVote, error) {
			gotReviewers = append(gotReviewers, *args.Reviewer.UniqueName)
			assert.Equal(t, 0, *args.Reviewer.Vote)
			return args.Reviewer, nil
		},
	)

	repo := newTestRepository(client)

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Test PR",
		Base:      "main",
		Head:      "feature",
		Labels:    []string{"label1", "label2"},
		Reviewers: []string{"reviewer@example.com"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1", "label2"}, gotLabels)
	assert.Equal(t, []string{"reviewer@example.com"}, gotReviewers)
}

func TestRepository_EditChange_addsLabelsAndReviewers(t *testing.T) {
	var gotLabels []string
	var gotReviewers []string
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().CreatePullRequestLabel(gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(
			_ context.Context,
			args git.CreatePullRequestLabelArgs,
		) (*core.WebApiTagDefinition, error) {
			gotLabels = append(gotLabels, *args.Label.Name)
			return &core.WebApiTagDefinition{Name: args.Label.Name}, nil
		},
	)
	client.EXPECT().CreateUnmaterializedPullRequestReviewer(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			args git.CreateUnmaterializedPullRequestReviewerArgs,
		) (*git.IdentityRefWithVote, error) {
			gotReviewers = append(gotReviewers, *args.Reviewer.UniqueName)
			return args.Reviewer, nil
		},
	)

	repo := newTestRepository(client)

	err := repo.EditChange(t.Context(), &PR{Number: 42}, forge.EditChangeOptions{
		AddLabels:    []string{"label1", "label2"},
		AddReviewers: []string{"reviewer@example.com"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1", "label2"}, gotLabels)
	assert.Equal(t, []string{"reviewer@example.com"}, gotReviewers)
}

func TestRepository_FindChangeByID_fetchesLabelsAndReviewers(t *testing.T) {
	prID := 42
	subject := "Test PR"
	status := git.PullRequestStatusValues.Active
	baseRef := "refs/heads/main"
	labelName := "label1"
	reviewerName := "reviewer@example.com"
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetPullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ git.GetPullRequestArgs,
		) (*git.GitPullRequest, error) {
			return &git.GitPullRequest{
				PullRequestId: &prID,
				Title:         &subject,
				Status:        &status,
				TargetRefName: &baseRef,
			}, nil
		},
	)
	client.EXPECT().GetPullRequestLabels(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ git.GetPullRequestLabelsArgs,
		) (*[]core.WebApiTagDefinition, error) {
			return &[]core.WebApiTagDefinition{
				{Name: &labelName},
			}, nil
		},
	)
	client.EXPECT().GetPullRequestReviewers(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ git.GetPullRequestReviewersArgs,
		) (*[]git.IdentityRefWithVote, error) {
			return &[]git.IdentityRefWithVote{
				{UniqueName: &reviewerName},
			}, nil
		},
	)

	repo := newTestRepository(client)

	got, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1"}, got.Labels)
	assert.Equal(t, []string{"reviewer@example.com"}, got.Reviewers)
}

func TestRepository_FindChangeByID_includesLabelsAndReviewers(t *testing.T) {
	prID := 42
	subject := "Test PR"
	status := git.PullRequestStatusValues.Active
	baseRef := "refs/heads/main"
	labelName := "label1"
	reviewerName := "reviewer@example.com"
	client := NewMockGitClient(gomock.NewController(t))
	client.EXPECT().GetPullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ git.GetPullRequestArgs,
		) (*git.GitPullRequest, error) {
			return &git.GitPullRequest{
				PullRequestId: &prID,
				Title:         &subject,
				Status:        &status,
				TargetRefName: &baseRef,
				Labels: &[]core.WebApiTagDefinition{
					{Name: &labelName},
				},
				Reviewers: &[]git.IdentityRefWithVote{
					{UniqueName: &reviewerName},
				},
			}, nil
		},
	)

	repo := newTestRepository(client)

	got, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1"}, got.Labels)
	assert.Equal(t, []string{"reviewer@example.com"}, got.Reviewers)
}
