package spice

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

func BenchmarkForgetBranch(b *testing.B) {
	for _, size := range []int{1, 100} {
		b.Run(fmt.Sprintf("Branches%d", size), func(b *testing.B) {
			ctx := b.Context()
			log := silogtest.New(b)
			repo, wt, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: log})
			require.NoError(b, err)
			db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
				Repo: repo, Ref: "refs/spice/data",
				AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com", Log: log,
			}))
			store, err := state.InitStore(ctx, state.InitStoreRequest{DB: db, Trunk: "main", Log: log})
			require.NoError(b, err)
			service := NewService(repo, wt, store, nil, log)
			sets := []storage.SetRequest{{
				Key: "branches/root", Value: jsontext.Value(`{"base":{"name":"main","hash":"initial"}}`),
			}}
			for i := 1; i < size; i++ {
				sets = append(sets, storage.SetRequest{
					Key:   fmt.Sprintf("branches/child-%03d", i),
					Value: jsontext.Value(`{"base":{"name":"root","hash":"parent"}}`),
				})
			}
			for b.Loop() {
				b.StopTimer()
				require.NoError(b, db.Update(ctx, storage.UpdateRequest{Sets: sets, Message: "restore graph"}))
				b.StartTimer()
				if err := service.ForgetBranch(ctx, "root"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
