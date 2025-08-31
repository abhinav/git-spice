package fixup

import (
	"bytes"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	gomock "go.uber.org/mock/gomock"
)

func TestFixupCommit_errors(t *testing.T) {
	// Error getting HEAD.
	t.Run("HeadError", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			Head(gomock.Any()).
			Return(git.Hash(""), assert.AnError)

		err := (&Handler{
			Log:        silogtest.New(t),
			Restack:    NewMockRestackHandler(mockCtrl),
			Worktree:   mockWorktree,
			Repository: NewMockGitRepository(mockCtrl),
			Service:    NewMockService(mockCtrl),
		}).FixupCommit(t.Context(), &Request{
			TargetHash:   "abc123",
			TargetBranch: "feat1",
			HeadBranch:   "feat2",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "determine HEAD")
	})

	// Target commit is not an ancestor of HEAD.
	t.Run("TargetNotAncestor", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			Head(gomock.Any()).
			Return("def456", nil)

		mockRepo := NewMockGitRepository(mockCtrl)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("def456")).
			Return(false)

		err := (&Handler{
			Log:        silogtest.New(t),
			Restack:    NewMockRestackHandler(mockCtrl),
			Worktree:   mockWorktree,
			Repository: mockRepo,
			Service:    NewMockService(mockCtrl),
		}).FixupCommit(t.Context(), &Request{
			TargetHash:   "abc123",
			TargetBranch: "feat1",
			HeadBranch:   "feat2",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "fixup commit must be an ancestor of HEAD")
	})

	// Target commit is on the trunk branch.
	t.Run("AlreadyOnTrunk", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			Head(gomock.Any()).
			Return("def456", nil)

		mockService := NewMockService(mockCtrl)
		mockService.EXPECT().
			Trunk().
			Return("main").
			AnyTimes()

		mockRepo := NewMockGitRepository(mockCtrl)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("def456")).
			Return(true)
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "main").
			Return(git.Hash("789abc"), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("789abc")).
			Return(true)

		err := (&Handler{
			Log:        silogtest.New(t),
			Restack:    NewMockRestackHandler(mockCtrl),
			Worktree:   mockWorktree,
			Repository: mockRepo,
			Service:    mockService,
		}).FixupCommit(t.Context(), &Request{
			TargetHash:   "abc123",
			TargetBranch: "feat1",
			HeadBranch:   "feat2",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot fixup a commit that has been merged into trunk")
	})

	// No changes staged for commit.
	t.Run("NoStagedChanges", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			Head(gomock.Any()).
			Return("def456", nil)
		mockWorktree.EXPECT().
			DiffIndex(gomock.Any(), "def456").
			Return([]git.FileStatus{}, nil)

		mockService := NewMockService(mockCtrl)
		mockService.EXPECT().
			Trunk().
			Return("main").
			AnyTimes()

		mockRepo := NewMockGitRepository(mockCtrl)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("def456")).
			Return(true)
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "main").
			Return(git.Hash("789abc"), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("789abc")).
			Return(false)

		err := (&Handler{
			Log:        silogtest.New(t),
			Restack:    NewMockRestackHandler(mockCtrl),
			Worktree:   mockWorktree,
			Repository: mockRepo,
			Service:    mockService,
		}).FixupCommit(t.Context(), &Request{
			TargetHash:   "abc123",
			TargetBranch: "feat1",
			HeadBranch:   "feat2",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "no changes staged for commit")
	})

	// Error diffing index.
	t.Run("DiffIndexError", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			Head(gomock.Any()).
			Return("def456", nil)
		mockWorktree.EXPECT().
			DiffIndex(gomock.Any(), "def456").
			Return(nil, assert.AnError)

		mockService := NewMockService(mockCtrl)
		mockService.EXPECT().
			Trunk().
			Return("main").
			AnyTimes()

		mockRepo := NewMockGitRepository(mockCtrl)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("def456")).
			Return(true)
		mockRepo.EXPECT().
			PeelToCommit(gomock.Any(), "main").
			Return(git.Hash("789abc"), nil)
		mockRepo.EXPECT().
			IsAncestor(gomock.Any(), git.Hash("abc123"), git.Hash("789abc")).
			Return(false)

		err := (&Handler{
			Log:        silogtest.New(t),
			Restack:    NewMockRestackHandler(mockCtrl),
			Worktree:   mockWorktree,
			Repository: mockRepo,
			Service:    mockService,
		}).FixupCommit(t.Context(), &Request{
			TargetHash:   "abc123",
			TargetBranch: "feat1",
			HeadBranch:   "feat2",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "diff index")
	})
}

func TestFixupCommit_success(t *testing.T) {
	gittest.SkipUnlessVersionAtLeast(t, gittest.Version{Major: 2, Minor: 45})

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		# main → feature1 → feature2
		#         (target)   (head)
		as 'Test Author <test@example.com>'
		at '2025-06-20T21:28:29Z'

		git init
		git add main.txt
		git commit -m 'Initial commit'

		git checkout -b feature1
		git add feature1.txt
		git commit -m 'Add feature1'

		git checkout -b feature2
		git add feature2.txt
		git commit -m 'Add feature2'

		# Stage changes for fixup
		git add staged.txt

		-- main.txt --
		main content

		-- feature1.txt --
		feature1 content

		-- feature2.txt --
		feature2 content

		-- staged.txt --
		staged content
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	log := silog.Nop()
	wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{Log: log})
	require.NoError(t, err)
	repo := wt.Repository()

	// feature1 is the target branch.
	feature1Hash, err := repo.PeelToCommit(t.Context(), "feature1")
	require.NoError(t, err)

	mockCtrl := gomock.NewController(t)

	mockService := NewMockService(mockCtrl)
	mockService.EXPECT().
		Trunk().
		Return("main").
		AnyTimes()

	// Will try to restack the upstack of feature1 (target branch)
	mockRestack := NewMockRestackHandler(mockCtrl)
	mockRestack.EXPECT().
		RestackUpstack(gomock.Any(), "feature1", gomock.Any()).
		Return(nil)

		// Sanity check: feature1:staged.txt does not exist yet.
	_, err = repo.HashAt(t.Context(), "feature1", "staged.txt")
	require.Error(t, err)
	assert.ErrorIs(t, err, git.ErrNotExist)

	err = (&Handler{
		Log:        log,
		Restack:    mockRestack,
		Worktree:   wt,
		Repository: repo,
		Service:    mockService,
	}).FixupCommit(t.Context(), &Request{
		TargetHash:   feature1Hash,
		TargetBranch: "feature1",
		HeadBranch:   "feature2",
	})
	require.NoError(t, err)

	// feature1:staged.txt now exists.
	blob, err := repo.HashAt(t.Context(), "feature1", "staged.txt")
	require.NoError(t, err)

	var got bytes.Buffer
	err = repo.ReadObject(t.Context(), git.BlobType, blob, &got)
	require.NoError(t, err)
	assert.Equal(t, "staged content\n", got.String())
}

type mapGraph map[string]spice.LoadBranchItem

func (m mapGraph) Lookup(name string) (spice.LoadBranchItem, bool) {
	item, ok := m[name]
	return item, ok
}

func (m mapGraph) Downstack(branch string) iter.Seq[string] {
	return func(yield func(string) bool) {
		current := branch
		for {
			item, ok := m[current]
			if !ok {
				return
			}

			if !yield(current) {
				return
			}

			if item.Base == "main" {
				return
			}

			current = item.Base
		}
	}
}

func TestFindCommitBranch(t *testing.T) {
	type findRequest struct{ from, give, want string }

	tests := []struct {
		name     string
		fixture  string
		bases    map[string]string
		requests []findRequest
	}{
		{
			name: "SimpleLinearBranch",
			fixture: `
				#  ┌─ feature1 (1 commit)
				# main (1 commit)
				as 'Test Author <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b feature1
				git add feature1.txt
				git commit -m 'Add feature1'

				-- main.txt --
				main content

				-- feature1.txt --
				feature1 content
			`,
			bases: map[string]string{
				"feature1": "main",
			},
			requests: []findRequest{
				{from: "feature1", give: "HEAD", want: "feature1"},
				{from: "feature1", give: "HEAD~1", want: ""},
			},
		},
		{
			name: "MultiBranchStack",
			fixture: `
				#    ┌─ feature2 (1 commit)
				#  ┌─┴ feature1 (1 commit)
				# main (1 commit)
				as 'Test Author <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b feature1
				git add feature1.txt
				git commit -m 'Add feature1'

				git checkout -b feature2
				git add feature2.txt
				git commit -m 'Add feature2'

				-- main.txt --
				main content

				-- feature1.txt --
				feature1 content

				-- feature2.txt --
				feature2 content
			`,
			bases: map[string]string{
				"feature1": "main",
				"feature2": "feature1",
			},
			requests: []findRequest{
				{from: "feature2", give: "HEAD~1", want: "feature1"},
				{from: "feature2", give: "HEAD", want: "feature2"},
				{from: "feature2", give: "HEAD~2", want: ""},
			},
		},
		{
			name: "BranchWithNoCommits",
			fixture: `
				#  ┌─ empty-branch (0 commits)
				# main (1 commit)
				as 'Test Author <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b empty-branch

				-- main.txt --
				main content
			`,
			bases: map[string]string{
				"empty-branch": "main",
			},
			requests: []findRequest{
				{from: "empty-branch", give: "HEAD", want: ""},
			},
		},
		{
			name: "TwoBranchesStackedOnOne",
			fixture: `
				#    ┌─ feature2 (1 commit)
				#    ├─ feature3 (1 commit)
				#  ┌─┴ feature1 (1 commit)
				# main (1 commit)
				as 'Test Author <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b feature1
				git add feature1.txt
				git commit -m 'Add feature1'

				git checkout -b feature2
				git add feature2.txt
				git commit -m 'Add feature2'

				git checkout feature1
				git checkout -b feature3
				git add feature3.txt
				git commit -m 'Add feature3'

				-- main.txt --
				main content

				-- feature1.txt --
				feature1 content

				-- feature2.txt --
				feature2 content

				-- feature3.txt --
				feature3 content
			`,
			bases: map[string]string{
				"feature1": "main",
				"feature2": "feature1",
				"feature3": "feature1",
			},
			requests: []findRequest{
				{from: "feature2", give: "HEAD~1", want: "feature1"},
				{from: "feature2", give: "HEAD", want: "feature2"},
				{from: "feature3", give: "HEAD", want: "feature3"},
				{from: "feature3", give: "HEAD~1", want: "feature1"},
				{from: "feature2", give: "HEAD~2", want: ""},
			},
		},
		{
			name: "MultipleCommitsPerBranch",
			fixture: `
				#    ┌─ feature2 (2 commit)
				#  ┌─┴ feature1 (3 commit)
				# main (1 commit)
				as 'Test Author <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b feature1
				git add feature1-1.txt
				git commit -m 'Add feature1 part 1'
				git add feature1-2.txt
				git commit -m 'Add feature1 part 2'
				git add feature1-3.txt
				git commit -m 'Add feature1 part 3'

				git checkout -b feature2
				git add feature2-1.txt
				git commit -m 'Add feature2 part 1'
				git add feature2-2.txt
				git commit -m 'Add feature2 part 2'

				-- main.txt --
				main content

				-- feature1-1.txt --
				feature1 part 1

				-- feature1-2.txt --
				feature1 part 2

				-- feature1-3.txt --
				feature1 part 3

				-- feature2-1.txt --
				feature2 part 1

				-- feature2-2.txt --
				feature2 part 2
			`,
			bases: map[string]string{
				"feature1": "main",
				"feature2": "feature1",
			},
			requests: []findRequest{
				// Test commits within feature1
				{from: "feature2", give: "HEAD~4", want: "feature1"}, // feature1 part 1
				{from: "feature2", give: "HEAD~3", want: "feature1"}, // feature1 part 2
				{from: "feature2", give: "HEAD~2", want: "feature1"}, // feature1 part 3
				// Test commits within feature2
				{from: "feature2", give: "HEAD~1", want: "feature2"}, // feature2 part 1
				{from: "feature2", give: "HEAD", want: "feature2"},   // feature2 part 2
				// Test main commit (should not be found)
				{from: "feature2", give: "HEAD~5", want: ""}, // main commit
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(tt.fixture)))
			require.NoError(t, err)
			t.Cleanup(fixture.Cleanup)

			log := silog.Nop()
			wt, err := git.OpenWorktree(t.Context(), fixture.Dir(), git.OpenOptions{Log: log})
			require.NoError(t, err)
			repo := wt.Repository()

			graph := make(mapGraph)
			for branch, base := range tt.bases {
				headHash, err := repo.PeelToCommit(t.Context(), branch)
				require.NoError(t, err)

				baseHash, err := repo.PeelToCommit(t.Context(), base)
				require.NoError(t, err)

				graph[branch] = spice.LoadBranchItem{
					Head:     headHash,
					Base:     base,
					BaseHash: baseHash,
				}
			}

			mockCtrl := gomock.NewController(t)
			handler := &Handler{
				Log:        log,
				Restack:    NewMockRestackHandler(mockCtrl),
				Repository: repo,
				Worktree:   wt,
				Service:    NewMockService(mockCtrl),
			}

			for _, req := range tt.requests {
				// Checkout the head branch to resolve HEAD references correctly
				err = wt.Checkout(t.Context(), req.from)
				require.NoError(t, err)

				giveHash, err := repo.PeelToCommit(t.Context(), req.give)
				require.NoError(t, err)

				gotBranch, err := handler.findCommitBranch(t.Context(), req.from, giveHash, graph)
				if req.want == "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "commit not found in any tracked branch")
				} else {
					require.NoError(t, err)
					assert.Equal(t, req.want, gotBranch)
				}
			}
		})
	}
}
