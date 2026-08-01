package github

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.uber.org/mock/gomock"
)

func TestRepository_MatchChangesToBranches_batchesInParallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		branches := make([]string, 101)
		for i := range branches {
			branches[i] = "branch-" + strconv.Itoa(i)
		}

		started := make(chan string, 2)
		release := make(chan struct{})
		gateway := NewMockGithubGateway(gomock.NewController(t))
		gateway.EXPECT().
			FindPullRequestsByBranches(gomock.Any(), gomock.Any()).
			DoAndReturn(func(
				_ context.Context,
				req *github.FindPullRequestsByBranchesRequest,
			) ([][]*github.PullRequestBranchMatch, error) {
				assert.Equal(t, "acme", req.Owner)
				assert.Equal(t, "repo", req.Repo)
				started <- req.Branches[0]
				<-release

				results := make([][]*github.PullRequestBranchMatch, len(req.Branches))
				for i, branch := range req.Branches {
					match := &github.PullRequestBranchMatch{
						ID:         github.ID("PR_" + branch),
						Number:     i + 1,
						State:      github.PullRequestStateOpen,
						HeadRefOID: "head-" + branch,
					}
					match.HeadRepository.Owner.Login = "acme"
					match.HeadRepository.Name = "repo"
					results[i] = []*github.PullRequestBranchMatch{match}
				}
				return results, nil
			}).
			Times(2)

		repo := &Repository{
			owner:   "acme",
			repo:    "repo",
			forge:   new(Forge),
			gateway: gateway,
		}
		resultC := make(chan []*forge.MatchChangesToBranchesResult, 1)
		go func() {
			resultC <- repo.MatchChangesToBranches(t.Context(), branches, nil)
		}()

		synctest.Wait()
		require.Len(t, started, 2, "batches should start in parallel")
		assert.ElementsMatch(
			t,
			[]string{"branch-0", "branch-100"},
			[]string{<-started, <-started},
		)

		close(release)
		synctest.Wait()
		got := <-resultC

		require.Len(t, got, len(branches))
		for i, result := range got {
			require.NoError(t, result.Err)
			require.Len(t, result.Changes, 1)
			assert.Equal(t, "#"+strconv.Itoa(i%100+1), result.Changes[0].ID.String())
			assert.Equal(t, "head-"+branches[i], string(result.Changes[0].HeadHash))
		}
	})
}

func TestRepository_MatchChangesToBranches_batchError(t *testing.T) {
	branches := make([]string, 201)
	for i := range branches {
		branches[i] = "branch-" + strconv.Itoa(i)
	}

	wantErr := errors.New("unavailable")
	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().
		FindPullRequestsByBranches(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			req *github.FindPullRequestsByBranchesRequest,
		) ([][]*github.PullRequestBranchMatch, error) {
			if req.Branches[0] == "branch-100" {
				return nil, wantErr
			}

			results := make([][]*github.PullRequestBranchMatch, len(req.Branches))
			for i, branch := range req.Branches {
				match := &github.PullRequestBranchMatch{
					ID:         github.ID("PR_" + branch),
					Number:     i + 1,
					State:      github.PullRequestStateMerged,
					HeadRefOID: "head-" + branch,
				}
				match.HeadRepository.Owner.Login = "acme"
				match.HeadRepository.Name = "repo"
				results[i] = []*github.PullRequestBranchMatch{match}
			}
			return results, nil
		}).
		Times(3)

	repo := &Repository{
		owner:   "acme",
		repo:    "repo",
		forge:   new(Forge),
		gateway: gateway,
	}
	got := repo.MatchChangesToBranches(t.Context(), branches, nil)

	assert.NoError(t, got[99].Err)
	assert.EqualError(t, got[100].Err, `match change to "branch-100": unavailable`)
	assert.ErrorIs(t, got[100].Err, wantErr)
	assert.EqualError(t, got[199].Err, `match change to "branch-199": unavailable`)
	assert.ErrorIs(t, got[199].Err, wantErr)
	assert.NotSame(t, got[100].Err, got[199].Err)
	require.NoError(t, got[200].Err)
	require.Len(t, got[200].Changes, 1)
}

func TestRepository_MatchChangesToBranches_filtersPushRepository(t *testing.T) {
	targetMatch := &github.PullRequestBranchMatch{
		ID: "PR_target", Number: 1, State: github.PullRequestStateOpen,
		HeadRefOID: "target",
	}
	targetMatch.HeadRepository.Owner.Login = "acme"
	targetMatch.HeadRepository.Name = "repo"
	forkMatch := &github.PullRequestBranchMatch{
		ID: "PR_fork", Number: 2, State: github.PullRequestStateOpen,
		HeadRefOID: "fork",
	}
	forkMatch.HeadRepository.Owner.Login = "other"
	forkMatch.HeadRepository.Name = "fork"

	gateway := NewMockGithubGateway(gomock.NewController(t))
	gateway.EXPECT().
		FindPullRequestsByBranches(
			gomock.Any(),
			&github.FindPullRequestsByBranchesRequest{
				Owner: "acme", Repo: "repo", Branches: []string{"feature"},
			},
		).
		Return([][]*github.PullRequestBranchMatch{{targetMatch, forkMatch}}, nil)

	repo := &Repository{
		owner:   "acme",
		repo:    "repo",
		forge:   new(Forge),
		gateway: gateway,
	}
	got := repo.MatchChangesToBranches(
		t.Context(),
		[]string{"feature"},
		&forge.MatchChangesToBranchesOptions{
			PushRepository: &RepositoryID{
				url:   DefaultURL,
				owner: "other",
				name:  "fork",
			},
		},
	)

	require.Len(t, got, 1)
	require.NoError(t, got[0].Err)
	require.Len(t, got[0].Changes, 1)
	assert.Equal(t, "#2", got[0].Changes[0].ID.String())
}
