package azuredevops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.uber.org/mock/gomock"
)

func TestRepository_SubmitChange_addsLabelsAndReviewers(t *testing.T) {
	var gotLabels []string
	var gotReviewers []string
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().CreatePullRequest(gomock.Any(), gomock.Any()).Return(
		&azuredevops.PullRequest{ID: 42}, nil,
	)
	gateway.EXPECT().AddLabel(gomock.Any(), "myproject", "myrepo", 42, gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, _, _ string, _ int, label string) error {
			gotLabels = append(gotLabels, label)
			return nil
		},
	)
	gateway.EXPECT().ReviewerID(gomock.Any(), "reviewer@example.com").Return("", nil)
	gateway.EXPECT().AddReviewerByName(
		gomock.Any(), "myproject", "myrepo", 42, "reviewer@example.com",
	).DoAndReturn(func(context.Context, string, string, int, string) error {
		gotReviewers = append(gotReviewers, "reviewer@example.com")
		return nil
	})

	repo := newTestRepository(gateway)

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
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().AddLabel(gomock.Any(), "myproject", "myrepo", 42, gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, _, _ string, _ int, label string) error {
			gotLabels = append(gotLabels, label)
			return nil
		},
	)
	gateway.EXPECT().ReviewerID(gomock.Any(), "reviewer@example.com").Return("", nil)
	gateway.EXPECT().AddReviewerByName(
		gomock.Any(), "myproject", "myrepo", 42, "reviewer@example.com",
	).DoAndReturn(func(context.Context, string, string, int, string) error {
		gotReviewers = append(gotReviewers, "reviewer@example.com")
		return nil
	})

	repo := newTestRepository(gateway)

	err := repo.EditChange(t.Context(), &PR{Number: 42}, forge.EditChangeOptions{
		AddLabels:    []string{"label1", "label2"},
		AddReviewers: []string{"reviewer@example.com"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1", "label2"}, gotLabels)
	assert.Equal(t, []string{"reviewer@example.com"}, gotReviewers)
}

func TestRepository_FindChangeByID_fetchesLabelsAndReviewers(t *testing.T) {
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().PullRequest(gomock.Any(), "myproject", "myrepo", 42).Return(
		&azuredevops.PullRequest{
			ID: 42, Title: "Test PR", Status: azuredevops.PullRequestStatusActive,
			TargetRef: "refs/heads/main",
		}, nil,
	)
	gateway.EXPECT().Labels(gomock.Any(), "myproject", "myrepo", 42).Return([]string{"label1"}, nil)
	gateway.EXPECT().Reviewers(gomock.Any(), "myproject", "myrepo", 42).Return([]string{"reviewer@example.com"}, nil)

	repo := newTestRepository(gateway)

	got, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1"}, got.Labels)
	assert.Equal(t, []string{"reviewer@example.com"}, got.Reviewers)
}

func TestRepository_FindChangeByID_includesLabelsAndReviewers(t *testing.T) {
	labels := []string{"label1"}
	reviewers := []string{"reviewer@example.com"}
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().PullRequest(gomock.Any(), "myproject", "myrepo", 42).Return(
		&azuredevops.PullRequest{
			ID: 42, Title: "Test PR", Status: azuredevops.PullRequestStatusActive,
			TargetRef: "refs/heads/main", Labels: &labels, Reviewers: &reviewers,
		}, nil,
	)

	repo := newTestRepository(gateway)

	got, err := repo.FindChangeByID(t.Context(), &PR{Number: 42})
	require.NoError(t, err)

	assert.Equal(t, []string{"label1"}, got.Labels)
	assert.Equal(t, []string{"reviewer@example.com"}, got.Reviewers)
}
