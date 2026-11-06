package azuredevops

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.uber.org/mock/gomock"
)

func TestRepository_SubmitChange_unsubmittedBase(t *testing.T) {
	createErr := errors.New("target branch not found")
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().CreatePullRequest(gomock.Any(), gomock.Any()).Return(nil, createErr)
	gateway.EXPECT().RefExists(gomock.Any(), "myproject", "myrepo", "heads/missing-base").Return(false, nil)

	repo := newTestRepository(gateway)

	_, err := repo.SubmitChange(t.Context(), forge.SubmitChangeRequest{
		Subject: "Test PR",
		Base:    "missing-base",
		Head:    "feature",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrUnsubmittedBase)
	assert.ErrorIs(t, err, createErr)
}

func TestRepository_UpdateChangeComment_notFound(t *testing.T) {
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().CommentExists(
		gomock.Any(), "myproject", "myrepo", 42, 7, 3,
	).Return(false, azuredevops.ErrNotFound)

	repo := newTestRepository(gateway)

	err := repo.UpdateChangeComment(t.Context(), &PRComment{
		PRID:      42,
		ThreadID:  7,
		CommentID: 3,
	}, "new body")
	require.Error(t, err)
	assert.ErrorIs(t, err, forge.ErrNotFound)
}
