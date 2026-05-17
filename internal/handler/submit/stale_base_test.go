package submit

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

func TestValidateStaleBaseCandidates_healthy(t *testing.T) {
	ctrl := gomock.NewController(t)

	graph := buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: submitFakeChange("pr-1")},
		{Name: "feat2", Base: "feat1", Change: submitFakeChange("pr-2")},
	})

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{
			submitFakeChangeID("pr-1"),
		}).
		Return([]forge.ChangeStatus{{State: forge.ChangeOpen}}, nil)

	count, err := validateStaleBaseCandidates(
		t.Context(),
		mockRepo,
		silog.Nop(),
		staleBaseCandidates(graph, []string{"feat2"}),
	)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestValidateStaleBaseCandidates_staleBase(t *testing.T) {
	ctrl := gomock.NewController(t)

	graph := buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: submitFakeChange("pr-1")},
		{Name: "feat2", Base: "feat1", Change: submitFakeChange("pr-2")},
	})

	mockRepo := forgetest.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		ChangeStatuses(gomock.Any(), []forge.ChangeID{
			submitFakeChangeID("pr-1"),
		}).
		Return([]forge.ChangeStatus{{State: forge.ChangeMerged}}, nil)

	var logBuffer bytes.Buffer
	count, err := validateStaleBaseCandidates(
		t.Context(),
		mockRepo,
		silog.New(&logBuffer, nil),
		staleBaseCandidates(graph, []string{"feat2"}),
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Contains(t, logBuffer.String(), "Branch has stale base")
	assert.Contains(t, logBuffer.String(), "branch=feat2")
	assert.Contains(t, logBuffer.String(), "base=feat1")
}

func TestStaleBaseCandidates_baseWithoutChange(t *testing.T) {
	graph := buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
		{Name: "feat1", Base: "main"},
		{Name: "feat2", Base: "feat1", Change: submitFakeChange("pr-2")},
	})

	assert.Empty(t, staleBaseCandidates(graph, []string{"feat2"}))
}

func TestStaleBaseCandidates_singleBranch(t *testing.T) {
	graph := buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: submitFakeChange("pr-1")},
	})

	assert.Empty(t, staleBaseCandidates(graph, []string{"feat1"}))
}

func TestValidateStaleBaseCandidates_deepStack(t *testing.T) {
	ctrl := gomock.NewController(t)

	// trunk <- A <- B <- C; A is merged.
	graph := buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
		{Name: "A", Base: "main", Change: submitFakeChange("pr-A")},
		{Name: "B", Base: "A", Change: submitFakeChange("pr-B")},
		{Name: "C", Base: "B", Change: submitFakeChange("pr-C")},
	})

	// Both A and B are checked because they are bases of B and C.
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

	count, err := validateStaleBaseCandidates(
		t.Context(),
		mockRepo,
		silog.Nop(),
		staleBaseCandidates(graph, []string{"C"}),
	)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestStaleBaseCandidates_deduplicatesBaseStatuses(t *testing.T) {
	graph := buildStaleBaseTestGraph(t, "main", []spice.LoadBranchItem{
		{Name: "feat1", Base: "main", Change: submitFakeChange("pr-1")},
		{Name: "feat2", Base: "feat1", Change: submitFakeChange("pr-2")},
		{Name: "feat3", Base: "feat2", Change: submitFakeChange("pr-3")},
	})

	candidates := staleBaseCandidates(graph, []string{"feat3", "feat2"})

	require.Len(t, candidates, 2)
	assert.Equal(t, submitFakeChangeID("pr-2"), candidates[0].ChangeID)
	assert.Equal(t, submitFakeChangeID("pr-1"), candidates[1].ChangeID)
}

func buildStaleBaseTestGraph(
	t *testing.T,
	trunk string,
	branches []spice.LoadBranchItem,
) *spice.BranchGraph {
	t.Helper()
	graph, err := spice.NewBranchGraph(t.Context(), &staleBaseBranchLoader{
		trunk:    trunk,
		branches: branches,
	}, nil)
	require.NoError(t, err)
	return graph
}

type staleBaseBranchLoader struct {
	trunk    string
	branches []spice.LoadBranchItem
}

func (l *staleBaseBranchLoader) Trunk() string { return l.trunk }

func (l *staleBaseBranchLoader) LoadBranches(
	context.Context,
) ([]spice.LoadBranchItem, error) {
	return l.branches, nil
}

func (*staleBaseBranchLoader) LookupWorktrees(
	context.Context,
	[]string,
) (map[string]string, error) {
	return nil, nil
}
