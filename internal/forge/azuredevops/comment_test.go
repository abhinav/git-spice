package azuredevops

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/azuredevops"
	"go.abhg.dev/gs/internal/silog"
	"go.uber.org/mock/gomock"
)

func TestRepository_ListChangeComments_canUpdate(t *testing.T) {
	currentUserID := "11111111-1111-1111-1111-111111111111"
	otherUserID := "22222222-2222-2222-2222-222222222222"
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().CurrentUserID(gomock.Any()).Return(currentUserID, nil)
	gateway.EXPECT().Threads(gomock.Any(), "myproject", "myrepo", 42).Return(
		[]azuredevops.Thread{{
			ID: 7,
			Comments: []azuredevops.Comment{
				{ID: 1, Body: "mine", AuthorID: currentUserID},
				{ID: 2, Body: "theirs", AuthorID: otherUserID},
			},
		}}, nil,
	)
	repo := newTestRepository(gateway)

	var got []*forge.ListChangeCommentItem
	for item, err := range repo.ListChangeComments(
		t.Context(), &PR{Number: 42}, &forge.ListChangeCommentsOptions{CanUpdate: true},
	) {
		require.NoError(t, err)
		got = append(got, item)
	}

	require.Len(t, got, 1)
	assert.Equal(t, "mine", got[0].Body)
}

func TestRepository_PostChangeComment(t *testing.T) {
	gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
	gateway.EXPECT().AddComment(
		gomock.Any(), "myproject", "myrepo", 42, "stack comment",
	).Return(10, 1, nil)
	repo := newTestRepository(gateway)

	got, err := repo.PostChangeComment(
		t.Context(), &PR{Number: 42}, "stack comment",
	)
	require.NoError(t, err)
	assert.Equal(t, &PRComment{PRID: 42, ThreadID: 10, CommentID: 1}, got)
}

func TestRepository_DeleteChangeComment(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
		gateway.EXPECT().DeleteComment(
			gomock.Any(), "myproject", "myrepo", 42, 7, 3,
		).Return(nil)

		repo := newTestRepository(gateway)

		err := repo.DeleteChangeComment(t.Context(), &PRComment{
			PRID:      42,
			ThreadID:  7,
			CommentID: 3,
		})
		require.NoError(t, err)
	})

	t.Run("Error", func(t *testing.T) {
		giveErr := errors.New("great sadness")
		gateway := NewMockAzureDevOpsGateway(gomock.NewController(t))
		gateway.EXPECT().DeleteComment(
			gomock.Any(), "myproject", "myrepo", 1, 2, 3,
		).Return(giveErr)

		repo := newTestRepository(gateway)

		err := repo.DeleteChangeComment(t.Context(), &PRComment{
			PRID:      1,
			ThreadID:  2,
			CommentID: 3,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, giveErr)
		assert.ErrorContains(t, err, "delete comment")
	})
}

func newTestRepository(gateway azureDevOpsGateway) *Repository {
	return &Repository{
		repoID: &RepositoryID{
			organization: "myorg",
			project:      "myproject",
			repository:   "myrepo",
		},
		log:     silog.Nop(),
		gateway: gateway,
	}
}
