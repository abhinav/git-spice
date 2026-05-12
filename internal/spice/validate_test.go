package spice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
)

// fakeChangeMetadata is a minimal forge.ChangeMetadata
// for use in tests.
type fakeChangeMetadata struct {
	id forge.ChangeID
}

var _ forge.ChangeMetadata = (*fakeChangeMetadata)(nil)

func (m *fakeChangeMetadata) ForgeID() string                              { return "test" }
func (m *fakeChangeMetadata) ChangeID() forge.ChangeID                     { return m.id }
func (m *fakeChangeMetadata) NavigationCommentID() forge.ChangeCommentID   { return nil }
func (m *fakeChangeMetadata) SetNavigationCommentID(forge.ChangeCommentID) {}

// fakeChangeID is a string-based ChangeID for testing.
type fakeChangeID string

func (f fakeChangeID) String() string { return string(f) }

func TestValidateDownstack_healthy(t *testing.T) {
	ctrl := gomock.NewController(t)

	graph := buildTestGraph(t, "main", []LoadBranchItem{
		{Name: "feat1", Base: "main", Change: fakeChange("pr-1")},
		{Name: "feat2", Base: "feat1", Change: fakeChange("pr-2")},
	})

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{fakeChangeID("pr-1")}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)

	err := ValidateDownstack(t.Context(), graph, mockRepo, "feat2")
	require.NoError(t, err)
}

func TestValidateDownstack_staleBase(t *testing.T) {
	ctrl := gomock.NewController(t)

	graph := buildTestGraph(t, "main", []LoadBranchItem{
		{Name: "feat1", Base: "main", Change: fakeChange("pr-1")},
		{Name: "feat2", Base: "feat1", Change: fakeChange("pr-2")},
	})

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{fakeChangeID("pr-1")}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	err := ValidateDownstack(t.Context(), graph, mockRepo, "feat2")

	var staleErr *StaleBaseError
	require.ErrorAs(t, err, &staleErr)
	assert.Equal(t, "feat2", staleErr.Branch)
	assert.Equal(t, "feat1", staleErr.Base)
}

func TestValidateDownstack_baseWithoutChange(t *testing.T) {
	graph := buildTestGraph(t, "main", []LoadBranchItem{
		{Name: "feat1", Base: "main"},
		{Name: "feat2", Base: "feat1", Change: fakeChange("pr-2")},
	})

	// No forge call expected: feat1 has no published change.
	err := ValidateDownstack(
		t.Context(), graph, nil /* unused */, "feat2",
	)
	require.NoError(t, err)
}

func TestValidateDownstack_singleBranch(t *testing.T) {
	graph := buildTestGraph(t, "main", []LoadBranchItem{
		{Name: "feat1", Base: "main", Change: fakeChange("pr-1")},
	})

	// No forge call expected: base is trunk.
	err := ValidateDownstack(
		t.Context(), graph, nil /* unused */, "feat1",
	)
	require.NoError(t, err)
}

func TestValidateDownstack_deepStack(t *testing.T) {
	ctrl := gomock.NewController(t)

	// trunk <- A <- B <- C; A is merged.
	graph := buildTestGraph(t, "main", []LoadBranchItem{
		{Name: "A", Base: "main", Change: fakeChange("pr-A")},
		{Name: "B", Base: "A", Change: fakeChange("pr-B")},
		{Name: "C", Base: "B", Change: fakeChange("pr-C")},
	})

	// Both A and B are checked (they are bases of B and C).
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

	err := ValidateDownstack(t.Context(), graph, mockRepo, "C")

	var staleErr *StaleBaseError
	require.ErrorAs(t, err, &staleErr)
	// B's base is A which is merged.
	assert.Equal(t, "B", staleErr.Branch)
	assert.Equal(t, "A", staleErr.Base)
}

// buildTestGraph constructs a BranchGraph from the given branches.
func buildTestGraph(
	t *testing.T,
	trunk string,
	branches []LoadBranchItem,
) *BranchGraph {
	t.Helper()
	graph, err := NewBranchGraph(t.Context(), &branchLoaderStub{
		trunk:    trunk,
		branches: branches,
	}, nil)
	require.NoError(t, err)
	return graph
}

// fakeChange creates a minimal ChangeMetadata
// for the given change ID.
func fakeChange(id string) forge.ChangeMetadata {
	return &fakeChangeMetadata{id: fakeChangeID(id)}
}
