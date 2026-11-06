package merge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submit"
	"go.abhg.dev/gs/internal/spice"
)

func TestMergeWorkspace_PrepareRange_bottomAfterBaseMerge(t *testing.T) {
	ctrl := gomock.NewController(t)
	service := NewMockService(ctrl)
	// The range starts above an earlier queue item that has now merged.
	// Its bottom still needs preparation onto trunk despite being first in range.
	service.EXPECT().VerifyRestacked(gomock.Any(), "bottom").
		Return(&spice.BranchNeedsRestackError{Base: "main"})
	restackHandler := NewMockRestackHandler(ctrl)
	restackHandler.EXPECT().RestackBranch(gomock.Any(), &restack.BranchRequest{
		Branch: "bottom",
	}).Return(nil)
	submitHandler := NewMockSubmitHandler(ctrl)
	submitHandler.EXPECT().Submit(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req *submit.Request) error {
			assert.Equal(t, "bottom", req.Branch)
			return nil
		})
	gitRepo := NewMockGitRepository(ctrl)
	gitRepo.EXPECT().PeelToCommit(gomock.Any(), "bottom").
		Return(git.Hash("restacked-bottom"), nil)

	workspace := &mergeWorkspace{
		Repository: gitRepo,
		Service:    service,
		Restack:    restackHandler,
		Submit:     submitHandler,
		Sync:       new(recordingSyncHandler),
		Progress:   new(recordingMergeProgress),
		Trunk:      "main",
		gate:       make(chan struct{}, 1),
	}
	item := testPlanEntry("bottom", "merged", "pr-bottom")
	item.headHash = "preflight-bottom"
	heads, err := workspace.PrepareRange(t.Context(), []*mergeItem{item})
	require.NoError(t, err)
	assert.Equal(t, []forge.ChangeHead{{
		Change: fakeChangeID("pr-bottom"), HeadHash: "restacked-bottom",
	}}, heads)
}
