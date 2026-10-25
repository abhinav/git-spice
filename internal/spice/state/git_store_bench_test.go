package state

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// benchmarkGitStore seeds sibling branches in one storage update.
// Transaction benchmarks create fresh transactions without committing, so every
// iteration starts with a cold transaction cache and the same persisted graph.
func benchmarkGitStore(b *testing.B, size int) (*Store, *storage.DB) {
	b.Helper()
	ctx := b.Context()
	log := silogtest.New(b)
	repo, _, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: log})
	require.NoError(b, err)
	db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
		Repo: repo, Ref: "refs/spice/data",
		AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com", Log: log,
	}))
	store, err := InitStore(ctx, InitStoreRequest{DB: db, Trunk: "main", Log: log})
	require.NoError(b, err)
	sets := make([]storage.SetRequest, size)
	for i := range sets {
		sets[i] = storage.SetRequest{
			Key:   branchKey(fmt.Sprintf("branch-%03d", i)),
			Value: &branchState{Base: branchStateBase{Name: "main", Hash: "initial"}},
		}
	}
	require.NoError(b, db.Update(ctx, storage.UpdateRequest{Sets: sets, Message: "seed branches"}))
	return store, db
}
