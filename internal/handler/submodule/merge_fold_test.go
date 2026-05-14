package submodule_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/handler/submodule"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
)

// fakeStore is a minimal in-memory implementation of submodule.ApplierStore.
type fakeStore struct {
	branches map[string]map[string]string
}

func (s *fakeStore) LookupBranch(
	_ context.Context, name string,
) (*state.LookupResponse, error) {
	subs, ok := s.branches[name]
	if !ok {
		return nil, state.ErrNotExist
	}
	return &state.LookupResponse{Submodules: subs}, nil
}

func TestMergeAssociationsForFold_disjointBase(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{
		"base":  {"libs/core": "sub-core"},
		"child": {"libs/util": "sub-util"},
	}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	resolved, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"libs/core": "sub-core",
		"libs/util": "sub-util",
	}, resolved)
}

func TestMergeAssociationsForFold_childWins(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{
		"base":  {},
		"child": {"libs/core": "feat-x"},
	}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	resolved, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
	})
	require.NoError(t, err)
	assert.Equal(t, "feat-x", resolved["libs/core"])
}

func TestMergeAssociationsForFold_sameValueKept(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{
		"base":  {"libs/core": "feat-x"},
		"child": {"libs/core": "feat-x"},
	}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	resolved, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
	})
	require.NoError(t, err)
	assert.Equal(t, "feat-x", resolved["libs/core"])
}

func TestMergeAssociationsForFold_conflictFlag(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{
		"base":  {"libs/core": "main"},
		"child": {"libs/core": "feat-x"},
	}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	resolved, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
		ModuleBranch: map[string]string{
			"libs/core": "feat-x",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "feat-x", resolved["libs/core"])
}

func TestMergeAssociationsForFold_conflictPrompt(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{
		"base":  {"libs/core": "main"},
		"child": {"libs/core": "feat-x"},
	}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	var prompted submodule.FoldConflict
	resolved, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
		Resolve: func(c submodule.FoldConflict) (string, error) {
			prompted = c
			return c.ChildBranch, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "feat-x", resolved["libs/core"])
	assert.Equal(t, "libs/core", prompted.Path)
	assert.Equal(t, "main", prompted.BaseBranch)
	assert.Equal(t, "feat-x", prompted.ChildBranch)
}

func TestMergeAssociationsForFold_conflictUnresolvedFails(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{
		"base":  {"libs/core": "main"},
		"child": {"libs/core": "feat-x"},
	}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	_, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
	})
	var conflictErr *submodule.FoldConflictError
	require.ErrorAs(t, err, &conflictErr)
	require.Len(t, conflictErr.Conflicts, 1)
	assert.Equal(t, "libs/core", conflictErr.Conflicts[0].Path)
}

func TestMergeAssociationsForFold_emptyOnBothMissing(t *testing.T) {
	t.Parallel()
	store := &fakeStore{branches: map[string]map[string]string{}}
	a := &submodule.Applier{Log: silogtest.New(t), Store: store}

	resolved, err := a.MergeAssociationsForFold(t.Context(), submodule.MergeFoldRequest{
		Base:  "base",
		Child: "child",
	})
	require.NoError(t, err)
	assert.Empty(t, resolved)
}
