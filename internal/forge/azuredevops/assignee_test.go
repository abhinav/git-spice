package azuredevops

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRepository_SubmitChange_ignoresAssignees(t *testing.T) {
	var logBuffer bytes.Buffer
	var gotInput *azuredevops.CreatePullRequestInput
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().CreatePullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input *azuredevops.CreatePullRequestInput) (*azuredevops.PullRequest, error) {
			gotInput = input
			return &azuredevops.PullRequest{ID: 42}, nil
		},
	)

	repo := newTestRepository(gateway)
	repo.log = silog.New(&logBuffer, nil)

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject:   "Test PR",
		Base:      "main",
		Head:      "feature",
		Assignees: []string{"alice"},
	})
	require.NoError(t, err)

	assert.NotNil(t, gotInput)
	assert.Contains(t, logBuffer.String(),
		"Azure DevOps does not support PR assignees; ignoring --assign flags")
}

func TestRepository_EditChange_ignoresAssignees(t *testing.T) {
	var logBuffer bytes.Buffer
	repo := newTestRepository(NewMockAzureDevOpsGateway(gomock.NewController(t)))
	repo.log = silog.New(&logBuffer, nil)

	err := repo.EditChange(t.Context(), &PR{Number: 42}, forge.EditChangeOptions{
		AddAssignees: []string{"alice"},
	})
	require.NoError(t, err)

	assert.Contains(t, logBuffer.String(),
		"Azure DevOps does not support PR assignees; ignoring --assign flags")
}

func TestRepository_EditChange_ignoresAssigneesWithOtherUpdates(t *testing.T) {
	var logBuffer bytes.Buffer
	var gotInput *azuredevops.UpdatePullRequestInput
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().UpdatePullRequest(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input *azuredevops.UpdatePullRequestInput) error {
			gotInput = input
			return nil
		},
	)

	repo := newTestRepository(gateway)
	repo.log = silog.New(&logBuffer, nil)

	err := repo.EditChange(t.Context(), &PR{Number: 42}, forge.EditChangeOptions{
		Base:         "develop",
		AddAssignees: []string{"alice"},
	})
	require.NoError(t, err)

	require.NotNil(t, gotInput.TargetRef)
	assert.Equal(t,
		"refs/heads/develop",
		*gotInput.TargetRef,
	)
	assert.Contains(t, logBuffer.String(),
		"Azure DevOps does not support PR assignees; ignoring --assign flags")
}
