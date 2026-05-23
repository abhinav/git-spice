package spice_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
)

func TestFindStaleBases_healthy(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{
			staleBaseFakeChangeID("pr-1"),
		}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)

	got, err := spice.FindStaleBases(
		t.Context(),
		buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
			{
				Name:   "feat1",
				Base:   "main",
				Change: staleBaseFakeChange("pr-1"),
			},
			{
				Name:   "feat2",
				Base:   "feat1",
				Change: staleBaseFakeChange("pr-2"),
			},
		}),
		staleBaseRepoOpener(mockRepo),
		[]string{"feat2"},
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFindStaleBases_staleBase(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{
			staleBaseFakeChangeID("pr-1"),
		}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	got, err := spice.FindStaleBases(
		t.Context(),
		buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
			{
				Name:   "feat1",
				Base:   "main",
				Change: staleBaseFakeChange("pr-1"),
			},
			{
				Name:   "feat2",
				Base:   "feat1",
				Change: staleBaseFakeChange("pr-2"),
			},
		}),
		staleBaseRepoOpener(mockRepo),
		[]string{"feat2"},
	)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "feat2", got[0].Branch)
	assert.Equal(t, "feat1", got[0].Base)
	assert.Equal(t, staleBaseFakeChangeID("pr-1"), got[0].ChangeID)
}

func TestFindStaleBases_baseWithoutChange(t *testing.T) {
	got, err := spice.FindStaleBases(
		t.Context(),
		buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
			{Name: "feat1", Base: "main"},
			{
				Name:   "feat2",
				Base:   "feat1",
				Change: staleBaseFakeChange("pr-2"),
			},
		}),
		staleBaseUnexpectedRepoOpener(t),
		[]string{"feat2"},
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFindStaleBases_singleBranch(t *testing.T) {
	got, err := spice.FindStaleBases(
		t.Context(),
		buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
			{
				Name:   "feat1",
				Base:   "main",
				Change: staleBaseFakeChange("pr-1"),
			},
		}),
		staleBaseUnexpectedRepoOpener(t),
		[]string{"feat1"},
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFindStaleBases_deepStack(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context, ids []forge.ChangeID,
		) ([]forge.ChangeStatus, error) {
			statuses := make([]forge.ChangeStatus, len(ids))
			for i, id := range ids {
				if id.String() == "pr-A" {
					statuses[i].State = forge.ChangeMerged
				} else {
					statuses[i].State = forge.ChangeOpen
				}
			}
			return statuses, nil
		})

	got, err := spice.FindStaleBases(
		t.Context(),
		buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
			{Name: "A", Base: "main", Change: staleBaseFakeChange("pr-A")},
			{Name: "B", Base: "A", Change: staleBaseFakeChange("pr-B")},
			{Name: "C", Base: "B", Change: staleBaseFakeChange("pr-C")},
		}),
		staleBaseRepoOpener(mockRepo),
		[]string{"C"},
	)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "B", got[0].Branch)
	assert.Equal(t, "A", got[0].Base)
}

func TestFindStaleBases_deduplicatesBaseStatuses(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{
			staleBaseFakeChangeID("pr-2"),
			staleBaseFakeChangeID("pr-1"),
		}).
		Return([]forge.ChangeStatus{
			{State: forge.ChangeOpen},
			{State: forge.ChangeOpen},
		}, nil)

	got, err := spice.FindStaleBases(
		t.Context(),
		buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
			{
				Name:   "feat1",
				Base:   "main",
				Change: staleBaseFakeChange("pr-1"),
			},
			{
				Name:   "feat2",
				Base:   "feat1",
				Change: staleBaseFakeChange("pr-2"),
			},
			{
				Name:   "feat3",
				Base:   "feat2",
				Change: staleBaseFakeChange("pr-3"),
			},
		}),
		staleBaseRepoOpener(mockRepo),
		[]string{"feat3", "feat2"},
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func buildStaleBaseTestGraph(
	t *testing.T,
	trunk string,
	branches []spice.LoadBranchItem,
) *spice.BranchGraph {
	t.Helper()
	return spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
		Trunk:    trunk,
		Branches: branches,
	})
}

func staleBaseRepoOpener(repo forge.Repository) func(context.Context) (forge.Repository, error) {
	return func(context.Context) (forge.Repository, error) {
		return repo, nil
	}
}

func staleBaseUnexpectedRepoOpener(
	t *testing.T,
) func(context.Context) (forge.Repository, error) {
	t.Helper()
	return func(context.Context) (forge.Repository, error) {
		require.FailNow(t, "forge repository should not be opened")
		return nil, nil
	}
}

type staleBaseFakeChangeID string

var _ forge.ChangeID = staleBaseFakeChangeID("")

func (id staleBaseFakeChangeID) String() string {
	return string(id)
}

type staleBaseFakeChange string

var _ forge.ChangeMetadata = staleBaseFakeChange("")

func (c staleBaseFakeChange) ForgeID() string {
	return "fake"
}

func (c staleBaseFakeChange) ChangeID() forge.ChangeID {
	return staleBaseFakeChangeID(c)
}

func (c staleBaseFakeChange) NavigationCommentID() forge.ChangeCommentID {
	return nil
}

func (c staleBaseFakeChange) SetNavigationCommentID(forge.ChangeCommentID) {}
