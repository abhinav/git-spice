package spice

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func BenchmarkLoadBranchesCleanup(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Children%d", size), func(b *testing.B) {
			ctx := b.Context()
			log := silogtest.New(b)
			repo, wt, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: log})
			require.NoError(b, err)
			db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
				Repo: repo, Ref: "refs/spice/data", Log: log,
				AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com",
			}))
			store, err := state.InitStore(ctx, state.InitStoreRequest{DB: db, Trunk: "main", Log: log})
			require.NoError(b, err)
			head, err := repo.PeelToCommit(ctx, "refs/spice/data")
			require.NoError(b, err)
			require.NoError(b, repo.CreateBranch(ctx, git.CreateBranchRequest{Name: "main", Head: head.String()}))
			sets := []storage.SetRequest{{Key: "branches/deleted", Value: map[string]any{
				"base": map[string]string{"name": "main", "hash": head.String()},
			}}}
			for i := range size {
				name := fmt.Sprintf("child-%03d", i)
				require.NoError(b, repo.CreateBranch(ctx, git.CreateBranchRequest{Name: name, Head: head.String()}))
				sets = append(sets, storage.SetRequest{Key: "branches/" + name, Value: map[string]any{
					"base": map[string]string{"name": "deleted", "hash": head.String()},
				}})
			}
			service := NewService(repo, wt, store, nil, log)
			for b.Loop() {
				b.StopTimer()
				require.NoError(b, db.Update(ctx, storage.UpdateRequest{Sets: sets, Message: "restore deleted base"}))
				b.StartTimer()
				items, err := service.LoadBranches(ctx)
				if err != nil {
					b.Fatal(err)
				}
				if len(items) != size {
					b.Fatalf("loaded %d branches, want %d", len(items), size)
				}
			}
		})
	}
}
