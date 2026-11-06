package storage

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/jsonmut"
	"go.abhg.dev/gs/internal/silog/silogtest"
)

func BenchmarkGitJSONMutation(b *testing.B) {
	for _, size := range []int{1, 100} {
		for _, required := range []bool{false, true} {
			b.Run(fmt.Sprintf("Branches%d/Required%t", size, required), func(b *testing.B) {
				ctx := b.Context()
				log := silogtest.New(b)
				repo, _, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: log})
				require.NoError(b, err)
				backend := NewGitBackend(GitConfig{
					Repo: repo, Ref: "refs/spice/data",
					AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com", Log: log,
				})
				sets := make([]SetRequest, size)
				for i := range sets {
					sets[i] = SetRequest{Key: fmt.Sprintf("branches/branch-%03d", i), Value: struct{}{}}
				}
				require.NoError(b, backend.Update(ctx, UpdateRequest{Sets: sets, Message: "seed branches"}))
				req := JSONMutationRequest{
					Key: "comments/branch-000", IfMissing: jsontext.Value(`{}`), Message: "edit draft",
				}
				if required {
					req.Requires = []string{"branches/branch-000"}
				}
				var iteration int
				for b.Loop() {
					body := jsontext.Value(strconv.Itoa(iteration))
					if err := UpdateJSON(ctx, backend, req, jsonmut.Set("/body", body)); err != nil {
						b.Fatal(err)
					}
					iteration++
				}
			})
		}
	}
}
