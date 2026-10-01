package continuation

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/autostash"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/spice/state"
)

func TestHandler_Continue(t *testing.T) {
	t.Run("Rebase", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			RebaseContinue(gomock.Any(), &git.RebaseContinueOptions{}).
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" continue").
			Return(nil, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Continue(t.Context(), &ContinueRequest{Edit: true})
		require.NoError(t, err)
	})

	t.Run("RebaseNoEdit", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			RebaseContinue(gomock.Any(), &git.RebaseContinueOptions{Editor: "true"}).
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" continue").
			Return(nil, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Continue(t.Context(), &ContinueRequest{Edit: false})
		require.NoError(t, err)
	})

	t.Run("Merge", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(nil, git.ErrNoRebase)
		mockWorktree.EXPECT().
			MergeState(gomock.Any()).
			Return(&git.MergeState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			MergeContinue(gomock.Any(), &git.MergeContinueOptions{Edit: true}).
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" continue").
			Return(nil, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Continue(t.Context(), &ContinueRequest{Edit: true})
		require.NoError(t, err)
	})

	t.Run("NoOperation", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(nil, git.ErrNoRebase)
		mockWorktree.EXPECT().
			MergeState(gomock.Any()).
			Return(nil, git.ErrNoMerge)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    NewMockStore(ctrl),
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Continue(t.Context(), &ContinueRequest{})
		assert.ErrorContains(t, err, "no operation to continue")
	})

	t.Run("StillConflicted", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		rebaseErr := &git.RebaseInterruptError{Kind: git.RebaseInterruptConflict}

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			RebaseContinue(gomock.Any(), gomock.Any()).
			Return(rebaseErr)

		var logBuf bytes.Buffer
		handler := &Handler{
			Log:      silog.New(&logBuf, nil),
			Worktree: mockWorktree,
			Store:    NewMockStore(ctrl),
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Continue(t.Context(), &ContinueRequest{})
		assert.ErrorIs(t, err, rebaseErr)
		assert.Contains(t, logBuf.String(), "more conflicts to resolve")
	})

	t.Run("RunsContinuation", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			RebaseContinue(gomock.Any(), gomock.Any()).
			Return(nil)
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" continue").
			Return([]state.Continuation{
				{Command: []string{"upstack", "restack"}, Branch: "feature"},
			}, nil)

		mockCommand := NewMockCommand(ctrl)
		mockCommand.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		mockParser := NewMockCommandParser(ctrl)
		mockParser.EXPECT().
			Parse([]string{"upstack", "restack"}).
			Return(mockCommand, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   mockParser,
		}

		err := handler.Continue(t.Context(), &ContinueRequest{Edit: true})
		require.NoError(t, err)
	})

	t.Run("ContinuationFails", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			RebaseContinue(gomock.Any(), gomock.Any()).
			Return(nil)
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "feature").
			Return(nil)

		conts := []state.Continuation{
			{Command: []string{"upstack", "restack"}, Branch: "feature"},
			{Command: []string{"branch", "submit"}, Branch: "feature2"},
		}

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" continue").
			Return(conts, nil)
		// The remainder is re-appended under the label it was taken with.
		mockStore.EXPECT().
			AppendContinuations(gomock.Any(), cli.Name()+" continue", conts[1:]).
			Return(nil)

		wantErr := errors.New("boom")
		mockCommand := NewMockCommand(ctrl)
		mockCommand.EXPECT().
			Run(gomock.Any()).
			Return(wantErr)

		mockParser := NewMockCommandParser(ctrl)
		mockParser.EXPECT().
			Parse([]string{"upstack", "restack"}).
			Return(mockCommand, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   mockParser,
		}

		err := handler.Continue(t.Context(), &ContinueRequest{Edit: true})
		assert.ErrorIs(t, err, wantErr)
	})
}

func TestHandler_Abort(t *testing.T) {
	t.Run("Rebase", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			RebaseAbort(gomock.Any()).
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" abort").
			Return(nil, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Abort(t.Context(), &AbortRequest{})
		require.NoError(t, err)
	})

	t.Run("Merge", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(nil, git.ErrNoRebase)
		mockWorktree.EXPECT().
			MergeState(gomock.Any()).
			Return(&git.MergeState{Branch: "feature"}, nil)
		mockWorktree.EXPECT().
			MergeAbort(gomock.Any()).
			Return(nil)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" abort").
			Return(nil, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Abort(t.Context(), &AbortRequest{})
		require.NoError(t, err)
	})

	t.Run("NoOperationNoContinuations", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(nil, git.ErrNoRebase)
		mockWorktree.EXPECT().
			MergeState(gomock.Any()).
			Return(nil, git.ErrNoMerge)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" abort").
			Return(nil, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Abort(t.Context(), &AbortRequest{})
		assert.ErrorContains(t, err, "no operation to abort")
	})

	t.Run("NoOperationDrainsContinuations", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(nil, git.ErrNoRebase)
		mockWorktree.EXPECT().
			MergeState(gomock.Any()).
			Return(nil, git.ErrNoMerge)

		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" abort").
			Return([]state.Continuation{
				{Command: []string{"upstack", "restack"}, Branch: "feature"},
			}, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   NewMockCommandParser(ctrl),
		}

		err := handler.Abort(t.Context(), &AbortRequest{})
		require.NoError(t, err)
	})

	t.Run("RestoresAutostash", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(nil, git.ErrNoRebase)
		mockWorktree.EXPECT().
			MergeState(gomock.Any()).
			Return(&git.MergeState{Branch: "feat2"}, nil)
		mockWorktree.EXPECT().
			MergeAbort(gomock.Any()).
			Return(nil)
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "feat1").
			Return(nil)

		restoreCmd := autostash.RestoreCommand("abc123")
		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" abort").
			Return([]state.Continuation{
				{Command: []string{"upstack", "restack"}, Branch: "feat1"},
				{Command: restoreCmd, Branch: "feat1"},
			}, nil)

		mockCommand := NewMockCommand(ctrl)
		mockCommand.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		mockParser := NewMockCommandParser(ctrl)
		mockParser.EXPECT().
			Parse(restoreCmd).
			Return(mockCommand, nil)

		handler := &Handler{
			Log:      silogtest.New(t),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   mockParser,
		}

		err := handler.Abort(t.Context(), &AbortRequest{})
		require.NoError(t, err)
	})

	t.Run("RestoresAutostashCheckoutFails", func(t *testing.T) {
		ctrl := gomock.NewController(t)

		mockWorktree := NewMockGitWorktree(ctrl)
		mockWorktree.EXPECT().
			RebaseState(gomock.Any()).
			Return(&git.RebaseState{Branch: "feat2"}, nil)
		mockWorktree.EXPECT().
			RebaseAbort(gomock.Any()).
			Return(nil)
		mockWorktree.EXPECT().
			CheckoutBranch(gomock.Any(), "feat1").
			Return(errors.New("branch is checked out elsewhere"))

		restoreCmd := autostash.RestoreCommand("abc123")
		mockStore := NewMockStore(ctrl)
		mockStore.EXPECT().
			TakeContinuations(gomock.Any(), cli.Name()+" abort").
			Return([]state.Continuation{
				{Command: restoreCmd, Branch: "feat1"},
			}, nil)

		mockCommand := NewMockCommand(ctrl)
		mockCommand.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		mockParser := NewMockCommandParser(ctrl)
		mockParser.EXPECT().
			Parse(restoreCmd).
			Return(mockCommand, nil)

		var logBuffer bytes.Buffer
		handler := &Handler{
			Log:      silog.New(&logBuffer, nil),
			Worktree: mockWorktree,
			Store:    mockStore,
			Parser:   mockParser,
		}

		err := handler.Abort(t.Context(), &AbortRequest{})
		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(), "Could not return to the branch")
	})
}
