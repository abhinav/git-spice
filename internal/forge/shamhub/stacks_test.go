package shamhub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
)

func executeStackUpdate(
	ctx context.Context,
	repository *stackRepository,
	changes []forge.StackChange,
) error {
	plan, err := repository.PlanStackUpdate(ctx, changes)
	if err != nil {
		return err
	}
	return plan.Execute(ctx)
}

func TestStackRepository_UpdateStack(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedStackChanges(sh)
	stacks := &stackRepository{forgeRepository: repo}

	require.NoError(t, executeStackUpdate(t.Context(), stacks, []forge.StackChange{
		{Change: ChangeID(3), BaseChange: ChangeID(1), BaseBranch: "bottom"},
		{Change: ChangeID(2), BaseChange: ChangeID(1), BaseBranch: "bottom"},
		{Change: ChangeID(1), BaseBranch: "main"},
	}))

	assert.Equal(t, map[int]int{1: 0, 2: 1, 3: 1},
		sh.stackBases[repoID{Owner: "alice", Name: "example"}])
}

func TestStackRepository_PlanStackUpdateIsReadOnly(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedStackChanges(sh)
	stacks := &stackRepository{forgeRepository: repo}

	plan, err := stacks.PlanStackUpdate(t.Context(), []forge.StackChange{
		{Change: ChangeID(2), BaseBranch: "main"},
	})
	require.NoError(t, err)
	assert.Equal(t, "bottom", sh.changes[1].Base.Name)
	require.NoError(t, plan.Execute(t.Context()))
	assert.Equal(t, "main", sh.changes[1].Base.Name)
}

func TestStackRepository_UpdateStack_replacesTouchedComponent(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedStackChanges(sh)
	stacks := &stackRepository{forgeRepository: repo}

	require.NoError(t, executeStackUpdate(t.Context(), stacks, []forge.StackChange{
		{Change: ChangeID(1), BaseBranch: "main"},
		{Change: ChangeID(2), BaseChange: ChangeID(1), BaseBranch: "bottom"},
		{Change: ChangeID(4), BaseChange: ChangeID(2), BaseBranch: "left"},
	}))
	require.NoError(t, executeStackUpdate(t.Context(), stacks, []forge.StackChange{
		{Change: ChangeID(2), BaseBranch: "main"},
		{Change: ChangeID(4), BaseChange: ChangeID(2), BaseBranch: "left"},
	}))

	assert.Equal(t, map[int]int{2: 0, 4: 2},
		sh.stackBases[repoID{Owner: "alice", Name: "example"}])
}

func TestStackRepository_PlanMergeRanges_usesStoredDivergence(t *testing.T) {
	sh, repo := newMergeabilityTestRepository(t)
	seedStackChanges(sh)
	stacks := &stackRepository{forgeRepository: repo}
	changes := []forge.StackChange{
		{Change: ChangeID(1), BaseBranch: "main"},
		{Change: ChangeID(2), BaseChange: ChangeID(1), BaseBranch: "bottom"},
		{Change: ChangeID(3), BaseChange: ChangeID(1), BaseBranch: "bottom"},
		{Change: ChangeID(4), BaseChange: ChangeID(2), BaseBranch: "left"},
	}
	require.NoError(t, executeStackUpdate(t.Context(), stacks, changes))

	plans, err := stacks.PlanMergeRanges(t.Context(), changes)
	require.NoError(t, err)
	require.Len(t, plans, 3)
	assert.Equal(t, []forge.ChangeID{ChangeID(1)}, plans[0].Changes())
	assert.Equal(t, []forge.ChangeID{ChangeID(2), ChangeID(4)}, plans[1].Changes())
	assert.Equal(t, []forge.ChangeID{ChangeID(3)}, plans[2].Changes())
}

func TestShamHub_UpdateStack_repositoryIsolation(t *testing.T) {
	sh, _ := newMergeabilityTestRepository(t)
	seedStackChanges(sh)
	sh.changes = append(sh.changes,
		shamChange{
			Number: 1,
			Base:   &shamBranch{Owner: "bob", Repo: "other", Name: "main"},
			Head:   &shamBranch{Owner: "bob", Repo: "other", Name: "bottom"},
		},
		shamChange{
			Number: 2,
			Base:   &shamBranch{Owner: "bob", Repo: "other", Name: "bottom"},
			Head:   &shamBranch{Owner: "bob", Repo: "other", Name: "top"},
		},
	)

	require.NoError(t, sh.updateStack("alice", "example", []stackChange{
		{Number: 1, BaseBranch: "main"},
		{Number: 2, Base: 1, BaseBranch: "bottom"},
	}))
	require.NoError(t, sh.updateStack("bob", "other", []stackChange{
		{Number: 1, BaseBranch: "main"},
		{Number: 2, Base: 1, BaseBranch: "bottom"},
	}))

	assert.Equal(t, map[int]int{1: 0, 2: 1},
		sh.stackBases[repoID{Owner: "alice", Name: "example"}])
	assert.Equal(t, map[int]int{1: 0, 2: 1},
		sh.stackBases[repoID{Owner: "bob", Name: "other"}])
}

func seedStackChanges(sh *ShamHub) {
	sh.changes = append(sh.changes,
		shamChange{
			Number: 1,
			Base:   &shamBranch{Owner: "alice", Repo: "example", Name: "main"},
			Head:   &shamBranch{Owner: "alice", Repo: "example", Name: "bottom"},
		},
		shamChange{
			Number: 2,
			Base:   &shamBranch{Owner: "alice", Repo: "example", Name: "bottom"},
			Head:   &shamBranch{Owner: "alice", Repo: "example", Name: "left"},
		},
		shamChange{
			Number: 3,
			Base:   &shamBranch{Owner: "alice", Repo: "example", Name: "bottom"},
			Head:   &shamBranch{Owner: "alice", Repo: "example", Name: "right"},
		},
		shamChange{
			Number: 4,
			Base:   &shamBranch{Owner: "alice", Repo: "example", Name: "left"},
			Head:   &shamBranch{Owner: "alice", Repo: "example", Name: "leaf"},
		},
	)
}
