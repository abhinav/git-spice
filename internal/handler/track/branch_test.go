package track

import (
	"bytes"
	"cmp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/spice/state/statetest"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

func TestHandler_TrackBranch(t *testing.T) {
	t.Run("CannotTrackTrunk", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)
		handler := &Handler{
			Log:        log,
			Repository: NewMockGitRepository(ctrl),
			Store:      store,
			Service:    NewMockService(ctrl),
			View:       &ui.FileView{W: t.Output()},
		}

		err := handler.TrackBranch(t.Context(), &BranchRequest{
			Branch: "main",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot track trunk branch")
	})

	t.Run("BaseSpecified", func(t *testing.T) {
		log := silog.Nop()
		store := statetest.NewMemoryStore(t, "main", "", log)

		// Track "develop" branch as base.
		require.NoError(t, statetest.UpdateBranch(t.Context(), store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:     "develop",
					Base:     "main",
					BaseHash: git.Hash("abc123"),
				},
			},
			Message: "add feature branch for test",
		}))

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "develop").
			Return(git.Hash("def456"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(nil)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
			View:       &ui.FileView{W: t.Output()},
		}

		err := handler.TrackBranch(t.Context(), &BranchRequest{
			Branch: "feature",
			Base:   "develop",
		})
		require.NoError(t, err)
	})

	t.Run("NoTrackedBranches", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "main").
			Return(git.Hash("def456"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").Return(nil)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
			View:       &ui.FileView{W: t.Output()},
		}

		err := handler.TrackBranch(t.Context(), &BranchRequest{
			Branch: "feature",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "feature: using base branch: main")
	})

	t.Run("GuessFailureDoesNotBreak", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctx := t.Context()
		err := statetest.UpdateBranch(ctx, store, &statetest.UpdateRequest{
			Upserts: []state.UpsertRequest{
				{
					Name:     "existing-branch",
					Base:     "main",
					BaseHash: git.Hash("existing123"),
				},
			},
			Message: "add existing branch for test",
		})
		require.NoError(t, err)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(ctx, "new-feature").
			Return("", assert.AnError)
		mockRepo.EXPECT().
			PeelToCommit(ctx, "main").
			Return(git.Hash("main456"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(ctx, "new-feature").
			Return(nil)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
			View:       &ui.FileView{W: t.Output()},
		}

		err = handler.TrackBranch(ctx, &BranchRequest{
			Branch: "new-feature",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "could not guess base branch, using trunk")
		assert.Contains(t, logBuffer.String(), "new-feature: using base branch: main")
	})

	t.Run("NeedsRestack", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "main").
			Return(git.Hash("main123"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(&spice.BranchNeedsRestackError{
				Base:     "main",
				BaseHash: git.Hash("main123"),
			})

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
			View:       &ui.FileView{W: t.Output()},
		}

		err := handler.TrackBranch(t.Context(), &BranchRequest{
			Branch: "feature",
			Base:   "main",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "branch is behind its base and needs to be restacked")
	})

	t.Run("RestackVerificationFailure", func(t *testing.T) {
		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, nil)
		store := statetest.NewMemoryStore(t, "main", "", log)

		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			PeelToCommit(t.Context(), "main").
			Return(git.Hash("main123"), nil)

		mockService := NewMockService(ctrl)
		mockService.EXPECT().
			VerifyRestacked(t.Context(), "feature").
			Return(assert.AnError)

		handler := &Handler{
			Log:        log,
			Repository: mockRepo,
			Store:      store,
			Service:    mockService,
			View:       &ui.FileView{W: t.Output()},
		}

		err := handler.TrackBranch(t.Context(), &BranchRequest{
			Branch: "feature",
			Base:   "main",
		})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "stack state verification failed")
	})
}

func TestGuessBaseBranch(t *testing.T) {
	type trackedBranch struct {
		name string
		base string // defaults to main
	}

	tests := []struct {
		name    string
		fixture string          // fixture script
		track   []trackedBranch // branches already tracked

		give string // branch to guess base for
		want string // expected base branch
	}{
		{
			name: "NoTrackedBranches",
			fixture: text.Dedent(`
				# main → feature
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b feature
				git add feature.txt
				git commit -m 'Add feature'

				-- main.txt --
				main content

				-- feature.txt --
				feature content
			`),
			give: "feature",
			want: "main",
		},
		{
			name: "FindDirectParentBranch",
			fixture: text.Dedent(`
				# main → develop → feature-base → feature
				#         (tracked)  (tracked)     (new)
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b develop
				git add develop.txt
				git commit -m 'Add develop'

				git checkout -b feature-base
				git add feature-base.txt
				git commit -m 'Add feature base'

				git checkout -b feature
				git add feature.txt
				git commit -m 'Add feature'

				-- main.txt --
				main content

				-- develop.txt --
				develop content

				-- feature-base.txt --
				feature base content

				-- feature.txt --
				feature content
			`),
			track: []trackedBranch{
				{name: "develop"},
				{name: "feature-base", base: "develop"},
			},
			give: "feature",
			want: "feature-base",
		},
		{
			name: "FindIndirectParentBranch",
			fixture: text.Dedent(`
				# main → release → release (more commits) → hotfix
				#         (tracked)                         (new)
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b release
				git add release.txt
				git commit -m 'Add release'

				git add release2.txt
				git commit -m 'Another release commit'

				git checkout -b hotfix
				git add hotfix.txt
				git commit -m 'Add hotfix'

				-- main.txt --
				main content

				-- release.txt --
				release content

				-- release2.txt --
				release2 content

				-- hotfix.txt --
				hotfix content
			`),
			track: []trackedBranch{
				{name: "release"},
			},
			give: "hotfix",
			want: "release",
		},
		{
			name: "FallBackToTrunk",
			fixture: text.Dedent(`
				# main ─┬─→ develop (tracked)
				#       └─→ feature-b (new, no tracked parent)
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b develop
				git add develop.txt
				git commit -m 'Add develop'

				git checkout main
				git checkout -b feature-b
				git add feature-b.txt
				git commit -m 'Add feature-b'

				-- main.txt --
				main content

				-- develop.txt --
				develop content

				-- feature-b.txt --
				feature-b content
			`),
			track: []trackedBranch{
				{name: "develop"},
			},
			give: "feature-b",
			want: "main",
		},
		{
			name: "IgnoreSameBranch",
			fixture: text.Dedent(`
				# main → develop → feature (both tracked, re-tracking feature)
				#         (tracked) (tracked, target)
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b develop
				git add develop.txt
				git commit -m 'Add develop'

				git checkout -b feature
				git add feature.txt
				git commit -m 'Add feature'

				-- main.txt --
				main content

				-- develop.txt --
				develop content

				-- feature.txt --
				feature content
			`),
			track: []trackedBranch{
				{name: "feature"},
				{name: "develop"},
			},
			give: "feature",
			want: "develop",
		},
		{
			name: "DeepBranchHierarchy",
			fixture: text.Dedent(`
				# main → level1 → level2 → level3 → feature
				#        (tracked) (tracked) (tracked) (new)
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b level1
				git add level1.txt
				git commit -m 'Add level1'

				git checkout -b level2
				git add level2.txt
				git commit -m 'Add level2'

				git checkout -b level3
				git add level3.txt
				git commit -m 'Add level3'

				git checkout -b feature
				git add feature.txt
				git commit -m 'Add feature'

				-- main.txt --
				main content

				-- level1.txt --
				level1 content

				-- level2.txt --
				level2 content

				-- level3.txt --
				level3 content

				-- feature.txt --
				feature content
			`),
			track: []trackedBranch{
				{name: "level1"},
				{name: "level2", base: "level1"},
				{name: "level3", base: "level2"},
			},
			give: "feature",
			want: "level3",
		},
		{
			name: "NestedFeatureBranches",
			fixture: text.Dedent(`
				# main → epic ─┬─→ story-a → task-1
				#      (tracked) │  (tracked)  (new)
				#                └─→ story-b
				#                   (tracked)
				as 'Test <test@example.com>'
				at '2025-06-20T21:28:29Z'

				git init
				git add main.txt
				git commit -m 'Initial commit'

				git checkout -b epic
				git add epic.txt
				git commit -m 'Add epic'

				git checkout -b story-a
				git add story-a.txt
				git commit -m 'Add story-a'

				git checkout epic
				git checkout -b story-b
				git add story-b.txt
				git commit -m 'Add story-b'

				git checkout story-a
				git checkout -b task-1
				git add task-1.txt
				git commit -m 'Add task-1'

				-- main.txt --
				main content

				-- epic.txt --
				epic content

				-- story-a.txt --
				story-a content

				-- story-b.txt --
				story-b content

				-- task-1.txt --
				task-1 content
			`),
			track: []trackedBranch{
				{name: "epic"},
				{name: "story-a", base: "epic"},
				{name: "story-b", base: "epic"},
			},
			give: "task-1",
			want: "story-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, err := gittest.LoadFixtureScript([]byte(tt.fixture))
			require.NoError(t, err)
			t.Cleanup(fixture.Cleanup)

			log := silog.Nop()
			store := statetest.NewMemoryStore(t, "main", fixture.Dir(), log)

			repo, err := git.Open(t.Context(), fixture.Dir(), git.OpenOptions{
				Log: log,
			})
			require.NoError(t, err)

			// Track specified branches
			for _, track := range tt.track {
				base := cmp.Or(track.base, "main")
				baseHash, err := repo.PeelToCommit(t.Context(), base)
				require.NoError(t, err)

				tx := store.BeginBranchTx()
				require.NoError(t, tx.Upsert(t.Context(), state.UpsertRequest{
					Name:     track.name,
					Base:     base,
					BaseHash: baseHash,
				}))
				require.NoError(t, tx.Commit(t.Context(), "track "+track.name))
			}

			result, err := guessBaseBranch(t.Context(), store, repo, tt.give)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}
