package sync

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
)

// fakeChangeID is a string-based ChangeID for testing.
type fakeChangeID string

func (f fakeChangeID) String() string { return string(f) }

// fakeChangeMetadata implements forge.ChangeMetadata for testing.
type fakeChangeMetadata struct {
	id forge.ChangeID
}

func (m *fakeChangeMetadata) ForgeID() string          { return "fake" }
func (m *fakeChangeMetadata) ChangeID() forge.ChangeID { return m.id }

func (m *fakeChangeMetadata) NavigationCommentID() forge.ChangeCommentID {
	return nil
}

func (m *fakeChangeMetadata) SetNavigationCommentID(forge.ChangeCommentID) {}

func TestCollectRetargetCandidates(t *testing.T) {
	branchGraph := func(branches []spice.LoadBranchItem) *spice.BranchGraph {
		return spicetest.NewBranchGraph(t, spicetest.BranchGraphConfig{
			Trunk:    "main",
			Branches: branches,
		})
	}

	t.Run("SingleDeletion", func(t *testing.T) {
		// main -> A -> B; A deleted, B survives with change.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{{BranchName: "A"}},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "main"},
				{
					Name:   "B",
					Base:   "A",
					Change: &fakeChangeMetadata{id: fakeChangeID("pr-2")},
				},
			}),
		))

		assert.Equal(t, []retargetCandidate{
			{branch: "B", changeID: fakeChangeID("pr-2"), newBase: "main"},
		}, got)
	})

	t.Run("MultiLevel", func(t *testing.T) {
		// main -> A -> B -> C; A and B deleted, C survives.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{
				{BranchName: "A"},
				{BranchName: "B"},
			},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "main"},
				{Name: "B", Base: "A"},
				{
					Name:   "C",
					Base:   "B",
					Change: &fakeChangeMetadata{id: fakeChangeID("pr-3")},
				},
			}),
		))

		assert.Equal(t, []retargetCandidate{
			{branch: "C", changeID: fakeChangeID("pr-3"), newBase: "main"},
		}, got)
	})

	t.Run("NoChange", func(t *testing.T) {
		// A deleted, B survives but has no published change.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{{BranchName: "A"}},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "main"},
				{Name: "B", Base: "A"},
			}),
		))

		assert.Empty(t, got)
	})

	t.Run("UpstackAlsoDeleted", func(t *testing.T) {
		// A and B both deleted — no retarget candidates.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{
				{BranchName: "A"},
				{BranchName: "B"},
			},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "main"},
				{
					Name:   "B",
					Base:   "A",
					Change: &fakeChangeMetadata{id: fakeChangeID("pr-2")},
				},
			}),
		))

		assert.Empty(t, got)
	})

	t.Run("BaseNotDeleted", func(t *testing.T) {
		// A is not deleted; B's base is not in the deletion set.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{{BranchName: "X"}},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "main"},
				{
					Name:   "B",
					Base:   "A",
					Change: &fakeChangeMetadata{id: fakeChangeID("pr-2")},
				},
			}),
		))

		assert.Empty(t, got)
	})

	t.Run("CyclicBases", func(t *testing.T) {
		// A -> B -> A (cycle), both deleted, C survives.
		// Should fall back to trunk instead of looping.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{
				{BranchName: "A"},
				{BranchName: "B"},
			},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "B"},
				{Name: "B", Base: "A"},
				{
					Name:   "C",
					Base:   "A",
					Change: &fakeChangeMetadata{id: fakeChangeID("pr-3")},
				},
			}),
		))

		assert.Equal(t, []retargetCandidate{
			{branch: "C", changeID: fakeChangeID("pr-3"), newBase: "main"},
		}, got)
	})

	t.Run("SurvivingNonTrunkAncestor", func(t *testing.T) {
		// main -> A -> B -> C; B deleted, A survives.
		// C should retarget to A, not main.
		got := slices.Collect(collectRetargetCandidates(
			[]branchDeletion{{BranchName: "B"}},
			branchGraph([]spice.LoadBranchItem{
				{Name: "A", Base: "main"},
				{Name: "B", Base: "A"},
				{
					Name:   "C",
					Base:   "B",
					Change: &fakeChangeMetadata{id: fakeChangeID("pr-3")},
				},
			}),
		))

		assert.Equal(t, []retargetCandidate{
			{branch: "C", changeID: fakeChangeID("pr-3"), newBase: "A"},
		}, got)
	})
}
