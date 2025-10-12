package autostash

import (
	"bytes"
	"errors"
	"fmt"
	reflect "reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice"
)

func TestHandler_AutoStash(t *testing.T) {
	t.Run("NoChanges", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			StashCreate(gomock.Any(), gomock.Any()).
			Return(git.Hash(""), git.ErrNoChanges)

		cleanup, err := (&Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Service:  NewMockService(mockCtrl),
		}).BeginAutostash(t.Context(), &Options{
			Message:   "autostash message",
			Branch:    "feature",
			ResetMode: ResetNone,
		})
		require.NoError(t, err)
		cleanup(nil) // should be no-op
	})

	t.Run("StashAndPop", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(mockCtrl)

		stashHash := git.Hash("stashhash")
		mockWorktree.EXPECT().
			StashCreate(gomock.Any(), "autostash message").
			Return(stashHash, nil)

		cleanup, err := (&Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Service:  NewMockService(mockCtrl),
		}).BeginAutostash(t.Context(), &Options{
			Message:   "autostash message",
			Branch:    "feature",
			ResetMode: ResetNone,
		})
		require.NoError(t, err)

		mockWorktree.EXPECT().
			StashApply(gomock.Any(), stashHash.String()).
			Return(nil)
		cleanup(nil)
	})

	t.Run("StashApplyError", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		mockWorktree := NewMockGitWorktree(mockCtrl)

		stashHash := git.Hash("stashhash")
		mockWorktree.EXPECT().
			StashCreate(gomock.Any(), "autostash message").
			Return(stashHash, nil)

		var logBuf bytes.Buffer
		cleanup, err := (&Handler{
			Log:      silog.New(&logBuf, nil),
			Worktree: mockWorktree,
			Service:  NewMockService(mockCtrl),
		}).BeginAutostash(t.Context(), &Options{
			Message:   "autostash message",
			Branch:    "feature",
			ResetMode: ResetNone,
		})
		require.NoError(t, err)

		stashErr := errors.New("sadness")
		mockWorktree.EXPECT().
			StashApply(gomock.Any(), stashHash.String()).
			Return(stashErr)

		mockWorktree.EXPECT().
			StashStore(gomock.Any(), stashHash, gomock.Any()).
			Return(nil)

		cleanup(new(error))

		assert.Contains(t, logBuf.String(), "Failed to apply autostash")
		assert.Contains(t, logBuf.String(), "apply them with 'git stash pop'")
	})
}

func TestHandler_AutoStash_reset(t *testing.T) {
	t.Run("Hard", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			StashCreate(gomock.Any(), gomock.Any()).
			Return(git.Hash("stashhash"), nil)
		mockWorktree.EXPECT().
			Reset(gomock.Any(), "HEAD", git.ResetOptions{
				Mode: git.ResetHard,
			}).Return(nil)
		mockWorktree.EXPECT().
			StashApply(gomock.Any(), gomock.Any()).
			Return(nil)

		cleanup, err := (&Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Service:  NewMockService(mockCtrl),
		}).BeginAutostash(t.Context(), &Options{
			Branch:    "feature",
			ResetMode: ResetHard,
		})
		require.NoError(t, err)
		cleanup(nil)
	})

	t.Run("Worktree", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			StashCreate(gomock.Any(), gomock.Any()).
			Return(git.Hash("stashhash"), nil)
		mockWorktree.EXPECT().
			CheckoutFiles(gomock.Any(), &git.CheckoutFilesRequest{
				Pathspecs: []string{"."},
			}).Return(nil)
		mockWorktree.EXPECT().
			StashApply(gomock.Any(), gomock.Any()).
			Return(nil)

		cleanup, err := (&Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Service:  NewMockService(mockCtrl),
		}).BeginAutostash(t.Context(), &Options{
			Branch:    "feature",
			ResetMode: ResetWorktree,
		})
		require.NoError(t, err)
		cleanup(nil)
	})
}

func TestHandler_AutoStash_options(t *testing.T) {
	t.Run("Branch", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(mockCtrl)
		mockWorktree.EXPECT().
			CurrentBranch(gomock.Any()).
			Return("feature", nil)

		stashHash := git.Hash("stashhash")
		mockWorktree.EXPECT().
			StashCreate(gomock.Any(), gomock.Any()).
			Return(stashHash, nil)

		mockService := NewMockService(mockCtrl)
		cleanup, err := (&Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Service:  mockService,
		}).BeginAutostash(t.Context(), &Options{
			ResetMode: ResetNone,
		})
		require.NoError(t, err)

		conflictErr := errors.New("sadness")
		mockService.EXPECT().
			RebaseRescue(gomock.Any(), rebaseRescueMatcher{
				Err:    conflictErr,
				Branch: "feature",
			}).Return(nil)
		cleanup(&conflictErr)
	})
}

type rebaseRescueMatcher struct {
	Err    error
	Cmd    []string // nil means don't match
	Branch string   // empty means don't match
}

var _ gomock.Matcher = rebaseRescueMatcher{}

func (i rebaseRescueMatcher) String() string {
	return fmt.Sprintf("RebaseRescueRequest{Err: %v, Command: %v, Branch: %q}", i.Err, i.Cmd, i.Branch)
}

func (i rebaseRescueMatcher) Matches(x any) bool {
	o, ok := x.(spice.RebaseRescueRequest)
	if !ok {
		return false
	}

	if want, got := i.Err, o.Err; !errors.Is(got, want) {
		return false
	}

	if want, got := i.Cmd, o.Command; want != nil && !reflect.DeepEqual(want, got) {
		return false
	}

	if want, got := i.Branch, o.Branch; want != "" && want != got {
		return false
	}

	return true
}
