package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	branchdel "go.abhg.dev/gs/internal/handler/delete"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

func TestHandler_deleteBranches_detachWorktreeFailure(t *testing.T) {
	t.Run("Open", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"blocked", "deletable"},
			}).
			Return(branchIter(
				git.LocalBranch{Name: "blocked", Worktree: "/missing"},
				git.LocalBranch{Name: "deletable"},
			))
		mockRepo.EXPECT().
			OpenWorktree(gomock.Any(), "/missing").
			Return(nil, errors.New("not found"))

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return("/repo")

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"deletable"},
				Force:    true,
			}).
			Return(nil)

		var logBuffer bytes.Buffer
		got, err := (&Handler{
			Log:        silog.New(&logBuffer, nil),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      NewMockStore(ctrl),
			Service:    NewMockService(ctrl),
			Delete:     mockDelete,
			Restack:    NewMockRestackHandler(ctrl),
			Autostash:  NewMockAutostashHandler(ctrl),
			Remote:     "origin",
		}).deleteBranches(t.Context(), []branchDeletion{
			{BranchName: "blocked"},
			{BranchName: "deletable"},
		}, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"deletable"}, got)
		assert.Contains(t, logBuffer.String(),
			"Unable to detach worktree; skipping branch deletion")
		assert.Contains(t, logBuffer.String(),
			"branch=blocked worktree=/missing error=open worktree: not found")
	})

	t.Run("Detach", func(t *testing.T) {
		fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
			as 'Test <test@example.com>'
			at '2026-08-30T18:30:00Z'

			mkdir repo
			cd repo
			git init
			git commit --allow-empty -m 'Initial commit'
			git worktree add ../blocked -b blocked
		`)))
		require.NoError(t, err)
		t.Cleanup(fixture.Cleanup)

		blockedPath := filepath.Join(fixture.Dir(), "blocked")
		blockedWorktree, err := git.OpenWorktree(t.Context(), blockedPath, git.OpenOptions{
			Log: silog.New(t.Output(), nil),
		})
		require.NoError(t, err)
		require.NoError(t, os.Rename(blockedPath, blockedPath+"-moved"))

		ctrl := gomock.NewController(t)
		mockRepo := NewMockGitRepository(ctrl)
		mockRepo.EXPECT().
			LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
				Patterns: []string{"blocked", "deletable"},
			}).
			Return(branchIter(
				git.LocalBranch{Name: "blocked", Worktree: blockedPath},
				git.LocalBranch{Name: "deletable"},
			))
		mockRepo.EXPECT().
			OpenWorktree(gomock.Any(), blockedPath).
			Return(blockedWorktree, nil)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RootDir().
			Return(filepath.Join(fixture.Dir(), "repo"))

		mockDelete := NewMockDeleteHandler(ctrl)
		mockDelete.EXPECT().
			DeleteBranches(gomock.Any(), &branchdel.Request{
				Branches: []string{"deletable"},
				Force:    true,
			}).
			Return(nil)

		var logBuffer bytes.Buffer
		got, err := (&Handler{
			Log:        silog.New(&logBuffer, nil),
			View:       ui.NewFileView(t.Output()),
			Repository: mockRepo,
			Worktree:   mockWorktree,
			Store:      NewMockStore(ctrl),
			Service:    NewMockService(ctrl),
			Delete:     mockDelete,
			Restack:    NewMockRestackHandler(ctrl),
			Autostash:  NewMockAutostashHandler(ctrl),
			Remote:     "origin",
		}).deleteBranches(t.Context(), []branchDeletion{
			{BranchName: "blocked"},
			{BranchName: "deletable"},
		}, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"deletable"}, got)
		assert.Contains(t, logBuffer.String(),
			"Unable to detach worktree; skipping branch deletion")
		assert.Contains(t, logBuffer.String(),
			"branch=blocked worktree="+blockedPath)
	})
}

func TestHandler_deleteBranches_logsDetachedHead(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2026-08-30T18:30:00Z'

		mkdir repo
		cd repo
		git init
		git commit --allow-empty -m 'Initial commit'
		git worktree add ../feature -b feature
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	featurePath := filepath.Join(fixture.Dir(), "feature")
	featureWorktree, err := git.OpenWorktree(t.Context(), featurePath, git.OpenOptions{
		Log: silog.New(t.Output(), nil),
	})
	require.NoError(t, err)
	wantHead, err := featureWorktree.Head(t.Context())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockRepo := NewMockGitRepository(ctrl)
	mockRepo.EXPECT().
		LocalBranches(gomock.Any(), &git.LocalBranchesOptions{
			Patterns: []string{"feature"},
		}).
		Return(singleBranchIter(git.LocalBranch{
			Name:     "feature",
			Hash:     "stale-branch-snapshot",
			Worktree: featurePath,
		}))
	mockRepo.EXPECT().
		OpenWorktree(gomock.Any(), featurePath).
		Return(featureWorktree, nil)

	mockWorktree := NewMockGitWorktree(ctrl)
	mockWorktree.EXPECT().
		RootDir().
		Return(filepath.Join(fixture.Dir(), "repo"))

	mockDelete := NewMockDeleteHandler(ctrl)
	mockDelete.EXPECT().
		DeleteBranches(gomock.Any(), &branchdel.Request{
			Branches: []string{"feature"},
			Force:    true,
		}).
		Return(nil)

	var logBuffer bytes.Buffer
	got, err := (&Handler{
		Log:        silog.New(&logBuffer, nil),
		View:       ui.NewFileView(t.Output()),
		Repository: mockRepo,
		Worktree:   mockWorktree,
		Store:      NewMockStore(ctrl),
		Service:    NewMockService(ctrl),
		Delete:     mockDelete,
		Restack:    NewMockRestackHandler(ctrl),
		Autostash:  NewMockAutostashHandler(ctrl),
		Remote:     "origin",
	}).deleteBranches(t.Context(), []branchDeletion{{
		BranchName: "feature",
	}}, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"feature"}, got)
	assert.Contains(t, logBuffer.String(),
		"feature: detached worktree "+featurePath+" at "+wantHead.String()+".")
	assert.NotContains(t, logBuffer.String(), "stale-branch-snapshot")
}
