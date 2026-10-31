package submit

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/forgetest"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/spicetest"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// BenchmarkNavigationComments includes rendering, worker coordination, real Git
// state reads, and commit. Forge calls return immediately, isolating local work
// from network latency. Existing comments need no metadata write and serve as
// a control. Fixture restoration and persisted-output checks are not timed.
func BenchmarkNavigationComments(b *testing.B) {
	for _, mode := range []string{"Post", "Existing"} {
		for _, size := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("%s%d", mode, size), func(b *testing.B) {
				ctx := b.Context()
				log := silogtest.New(b)
				repo, _, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: log})
				require.NoError(b, err)
				reads := &navigationBenchmarkRepository{Repository: repo}
				db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
					Repo: reads, Ref: "refs/spice/data", Log: log,
					AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com",
				}))
				store, err := state.InitStore(ctx, state.InitStoreRequest{DB: db, Trunk: "main", Log: log})
				require.NoError(b, err)
				items := make([]spice.LoadBranchItem, size)
				names := make([]string, size)
				tx := store.BeginBranchTx()
				base := "main"
				for i := range items {
					name := fmt.Sprintf("branch-%03d", i)
					meta := &shamhub.ChangeMetadata{Number: i + 1}
					if mode == "Existing" {
						meta.NavigationComment = i + 1
					}
					items[i] = spice.LoadBranchItem{Name: name, Base: base, Change: meta}
					names[i] = name
					encoded, err := json.Marshal(meta)
					require.NoError(b, err)
					require.NoError(b, tx.Upsert(ctx, state.UpsertRequest{
						Name: name, Base: base, ChangeForge: "shamhub", ChangeMetadata: encoded,
					}))
					base = name
				}
				require.NoError(b, tx.Commit(ctx, "seed submitted stack"))
				seed, err := repo.PeelToCommit(ctx, "refs/spice/data")
				require.NoError(b, err)
				graph := spicetest.NewBranchGraph(b, spicetest.BranchGraphConfig{Trunk: "main", Branches: items})

				ctrl := gomock.NewController(b)
				remote := forgetest.NewMockRepository(ctrl)
				remote.EXPECT().Forge().Return(new(shamhub.Forge)).AnyTimes()
				remote.EXPECT().PostChangeComment(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, id forge.ChangeID, _ string) (forge.ChangeCommentID, error) {
						return shamhub.ChangeCommentID(id.(shamhub.ChangeID)), nil
					}).AnyTimes()
				remote.EXPECT().UpdateChangeComment(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				var objectReads int
				for b.Loop() {
					b.StopTimer()
					require.NoError(b, repo.SetRef(ctx, git.SetRefRequest{Ref: "refs/spice/data", Hash: seed}))
					if mode == "Post" {
						for _, item := range items {
							item.Change.(*shamhub.ChangeMetadata).NavigationComment = 0
						}
					}
					reads.calls = 0
					b.StartTimer()
					err := updateNavigationComments(ctx, navigationCommentUpdate{
						store: store, graph: graph, log: log, remoteRepository: remote,
						when: NavCommentAlways, sync: NavCommentSyncBranch, submittedBranches: names,
					})
					b.StopTimer()
					objectReads += reads.calls
					require.NoError(b, err)
					for i, name := range names {
						got, err := store.LookupBranch(ctx, name)
						require.NoError(b, err)
						var meta shamhub.ChangeMetadata
						require.NoError(b, json.Unmarshal(got.ChangeMetadata, &meta))
						require.Equal(b, i+1, meta.Number)
						require.Equal(b, i+1, meta.NavigationComment)
					}
					b.StartTimer()
				}
				b.ReportMetric(float64(objectReads)/float64(b.N), "cat-file/op")
			})
		}
	}
}

// Each ReadObject or ReadObjects invocation starts one cat-file process.
// Store access is serialized by the transaction or runs after workers finish.
type navigationBenchmarkRepository struct {
	*git.Repository

	calls int
}

func (r *navigationBenchmarkRepository) ReadObject(ctx context.Context, typ git.Type, hash git.Hash, dst io.Writer) error {
	r.calls++
	return r.Repository.ReadObject(ctx, typ, hash, dst)
}

func (r *navigationBenchmarkRepository) ReadObjects(ctx context.Context, hashes []git.Hash) (*git.ObjectBatchReader, error) {
	r.calls++
	return r.Repository.ReadObjects(ctx, hashes)
}
