package sync

import (
	"context"
	"fmt"
	"io"
	"strconv"
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
	"go.abhg.dev/gs/internal/ui"
)

// BenchmarkSyncMergedHistory propagates a merged prefix to a surviving tip.
// Real Git reads, propagation, and the state commit are timed;
// fixture restoration, graph loading, and output checks are not.
// Forge status lookup is deterministic and has no network latency.
func BenchmarkSyncMergedHistory(b *testing.B) {
	for _, size := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("Merged%d", size), func(b *testing.B) {
			ctx := b.Context()
			log := silogtest.New(b)
			repo, wt, err := git.Init(ctx, b.TempDir(), git.InitOptions{Log: log})
			require.NoError(b, err)
			reads := &historyBenchmarkRepository{Repository: repo}
			db := storage.NewDB(storage.NewGitBackend(storage.GitConfig{
				Repo: reads, Ref: "refs/spice/data", Log: log,
				AuthorName: "Benchmark", AuthorEmail: "benchmark@example.com",
			}))
			store, err := state.InitStore(ctx, state.InitStoreRequest{DB: db, Trunk: "main", Log: log})
			require.NoError(b, err)
			head, err := repo.PeelToCommit(ctx, "refs/spice/data")
			require.NoError(b, err)
			tree, err := repo.PeelToTree(ctx, head.String())
			require.NoError(b, err)
			sig := &git.Signature{Name: "Benchmark", Email: "benchmark@example.com"}
			tip, err := repo.CommitTree(ctx, git.CommitTreeRequest{
				Tree: tree, Parents: []git.Hash{head}, Message: "unmerged tip",
				Author: sig, Committer: sig,
			})
			require.NoError(b, err)

			items := make([]spice.LoadBranchItem, size+1)
			statuses := make([]forge.ChangeStatus, len(items))
			tx := store.BeginBranchTx()
			base := "main"
			for i := range items {
				name := fmt.Sprintf("branch-%03d", i)
				items[i] = spice.LoadBranchItem{
					Name: name, Base: base, BaseHash: head, Head: head,
					Change: &shamhub.ChangeMetadata{Number: i + 1},
				}
				statuses[i] = forge.ChangeStatus{State: forge.ChangeMerged, HeadHash: head}
				require.NoError(b, tx.Upsert(ctx, state.UpsertRequest{Name: name, Base: base, BaseHash: head}))
				base = name
			}
			// The surviving tip must not be reachable from trunk, regardless
			// of the forge reporting its change as open.
			items[size].Head = tip
			statuses[size].State = forge.ChangeOpen
			statuses[size].HeadHash = tip
			require.NoError(b, tx.Commit(ctx, "seed stack"))
			seed, err := repo.PeelToCommit(ctx, "refs/spice/data")
			require.NoError(b, err)
			graph := spicetest.NewBranchGraph(b, spicetest.BranchGraphConfig{Trunk: "main", Branches: items})

			ctrl := gomock.NewController(b)
			remote := forgetest.NewMockRepository(ctrl)
			remote.EXPECT().Forge().Return(new(shamhub.Forge)).AnyTimes()
			remote.EXPECT().ChangeStatuses(gomock.Any(), gomock.Any()).Return(statuses, nil).AnyTimes()
			handler := &Handler{
				Store: store, Repository: repo, RemoteRepository: remote, Log: log,
				View: ui.NewFileView(b.Output()), Worktree: wt, Remote: "origin",
				Service: NewMockService(ctrl), Delete: NewMockDeleteHandler(ctrl),
				Restack: NewMockRestackHandler(ctrl), Autostash: NewMockAutostashHandler(ctrl),
			}
			var objectReads int
			for b.Loop() {
				b.StopTimer()
				require.NoError(b, repo.SetRef(ctx, git.SetRefRequest{Ref: "refs/spice/data", Hash: seed}))
				reads.calls = 0
				b.StartTimer()
				deletions, err := handler.findForgeFinishedBranches(ctx, graph, items, head, ClosedChangesIgnore)
				b.StopTimer()
				objectReads += reads.calls
				require.NoError(b, err)
				require.Len(b, deletions, size)
				// Read through the store, not the input graph, to verify the commit.
				for i, item := range items {
					got, err := store.LookupBranch(ctx, item.Name)
					require.NoError(b, err)
					require.Len(b, got.MergedDownstack, i)
					for j, value := range got.MergedDownstack {
						require.JSONEq(b, strconv.Itoa(j+1), string(value))
					}
				}
				b.StartTimer()
			}
			b.ReportMetric(float64(objectReads)/float64(b.N), "cat-file/op")
		})
	}
}

// Each ReadObject or ReadObjects invocation starts one cat-file process.
// Count only the Git-backed store's reads during the timed operation.
type historyBenchmarkRepository struct {
	*git.Repository

	calls int
}

func (r *historyBenchmarkRepository) ReadObject(ctx context.Context, typ git.Type, hash git.Hash, dst io.Writer) error {
	r.calls++
	return r.Repository.ReadObject(ctx, typ, hash, dst)
}

func (r *historyBenchmarkRepository) ReadObjects(ctx context.Context, hashes []git.Hash) (*git.ObjectBatchReader, error) {
	r.calls++
	return r.Repository.ReadObjects(ctx, hashes)
}
