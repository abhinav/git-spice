package storage

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
)

// BenchmarkGitBackendUpdate includes serialization, blob and tree writes,
// commit creation, and ref publication. Revisions change every iteration
// so the benchmark measures new objects rather than no-op updates.
func BenchmarkGitBackendUpdate(b *testing.B) {
	for _, count := range []int{0, 1, 10, 100} {
		for _, payload := range []int{512, 8192} {
			b.Run(fmt.Sprintf("Objects%d/NoteBytes%d", count, payload), func(b *testing.B) {
				ctx := b.Context()
				repo, _, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: silog.Nop()})
				require.NoError(b, err)
				backend := NewGitBackend(GitConfig{
					Repo: repo, Ref: "refs/spice/data",
					AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com", Log: silog.Nop(),
				})
				type value struct {
					Revision string `json:"revision"`
					Note     string `json:"note"`
				}
				values := make([]value, count)
				sets := make([]SetRequest, count)
				random := rand.New(rand.NewPCG(1, 2))
				const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
				for i := range values {
					note := make([]byte, payload)
					for j := range note {
						note[j] = alphabet[random.IntN(len(alphabet))]
					}
					values[i] = value{Revision: "00000000000000000000", Note: string(note)}
					sets[i] = SetRequest{Key: fmt.Sprintf("branches/branch-%03d", i), Value: &values[i]}
				}
				require.NoError(b, backend.Update(ctx, UpdateRequest{Sets: sets, Message: "seed branches"}))
				var revision int
				for b.Loop() {
					revision++
					stamp := fmt.Sprintf("%020d", revision)
					for i := range values {
						values[i].Revision = stamp
					}
					if err := backend.Update(ctx, UpdateRequest{Sets: sets, Message: "update branches"}); err != nil {
						b.Fatal(err)
					}
				}
				for i, set := range sets {
					var got value
					require.NoError(b, backend.Get(ctx, set.Key, &got))
					require.Equal(b, values[i], got)
				}
			})
		}
	}
}
