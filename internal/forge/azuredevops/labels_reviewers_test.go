package azuredevops

import (
	"context"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func TestRepository_SubmitChange_addsLabelsAndReviewers(t *testing.T) {
	var gotLabels []string
	var gotReviewers []string
	stub := &stubGitClient{
		createPullRequest: func(
			_ context.Context,
			_ git.CreatePullRequestArgs,
		) (*git.GitPullRequest, error) {
			prID := 42
			return &git.GitPullRequest{PullRequestId: &prID}, nil
		},
		createPullRequestLabel: func(
			_ context.Context,
			args git.CreatePullRequestLabelArgs,
		) (*core.WebApiTagDefinition, error) {
			gotLabels = append(gotLabels, *args.Label.Name)
			return &core.WebApiTagDefinition{Name: args.Label.Name}, nil
		},
		createUnmaterializedPullRequestReviewer: func(
			_ context.Context,
			args git.CreateUnmaterializedPullRequestReviewerArgs,
		) (*git.IdentityRefWithVote, error) {
			gotReviewers = append(gotReviewers, *args.Reviewer.UniqueName)
			assert.Equal(t, 0, *args.Reviewer.Vote)
			return args.Reviewer, nil
		},
	}

	repo := newTestRepository(stub)

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
	stub := &stubGitClient{
		createPullRequestLabel: func(
			_ context.Context,
			args git.CreatePullRequestLabelArgs,
		) (*core.WebApiTagDefinition, error) {
			gotLabels = append(gotLabels, *args.Label.Name)
			return &core.WebApiTagDefinition{Name: args.Label.Name}, nil
		},
		createUnmaterializedPullRequestReviewer: func(
			_ context.Context,
			args git.CreateUnmaterializedPullRequestReviewerArgs,
		) (*git.IdentityRefWithVote, error) {
			gotReviewers = append(gotReviewers, *args.Reviewer.UniqueName)
			return args.Reviewer, nil
		},
	}

	repo := newTestRepository(stub)

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
	stub := &stubGitClient{
		getPullRequest: func(
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
		getPullRequestLabels: func(
			_ context.Context,
			_ git.GetPullRequestLabelsArgs,
		) (*[]core.WebApiTagDefinition, error) {
			return &[]core.WebApiTagDefinition{
				{Name: &labelName},
			}, nil
		},
		getPullRequestReviewers: func(
			_ context.Context,
			_ git.GetPullRequestReviewersArgs,
		) (*[]git.IdentityRefWithVote, error) {
			return &[]git.IdentityRefWithVote{
				{UniqueName: &reviewerName},
			}, nil
		},
	}

	repo := newTestRepository(stub)

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
	stub := &stubGitClient{
		getPullRequest: func(
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
	}

	repo := newTestRepository(stub)

	got, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1"}, got.Labels)
	assert.Equal(t, []string{"reviewer@example.com"}, got.Reviewers)
}
