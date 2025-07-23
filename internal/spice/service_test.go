package spice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
	"go.uber.org/mock/gomock"
)

// NewTestService creates a new Service for testing.
// If forge is nil, it uses the ShamHub forge.
func NewTestService(
	repo GitRepository,
	wt GitWorktree,
	store Store,
	forgeReg *forge.Registry,
	log *silog.Logger,
) *Service {
	return newService(repo, wt, store, forgeReg, log)
}

// NewMemoryStore builds gs state storage
// that stores everything in memory.
// The store is initialized with the trunk "main".
func NewMemoryStore(t *testing.T) *state.Store {
	t.Helper()

	ctx := t.Context()
	db := storage.NewDB(make(storage.MapBackend))
	store, err := state.InitStore(ctx, state.InitStoreRequest{
		DB:    db,
		Trunk: "main",
		Log:   silogtest.New(t),
	})
	require.NoError(t, err)

	return store
}

func TestService_LookupWorktrees(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockRepo := NewMockGitRepository(ctrl)
	mockWorktree := NewMockGitWorktree(ctrl)
	store := NewMemoryStore(t)
	log := silogtest.New(t)

	svc := NewTestService(mockRepo, mockWorktree, store, nil, log)

	feature1WT := t.TempDir()
	feature3WT := t.TempDir()
	mockRepo.EXPECT().
		LocalBranches(gomock.Any(), gomock.Any()).
		Return(sliceutil.All2[error]([]git.LocalBranch{
			{Name: "feature1", Worktree: feature1WT},
			{Name: "feature2"},
			{Name: "feature3", Worktree: feature3WT},
			{Name: "random", Worktree: t.TempDir()},
		}))

	got, err := svc.LookupWorktrees(t.Context(), []string{"feature1", "feature2", "feature3"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"feature1": feature1WT,
		"feature3": feature3WT,
	}, got)
}
