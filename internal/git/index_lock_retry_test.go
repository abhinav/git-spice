package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/retry"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/xec"
	"go.uber.org/mock/gomock"
)

func TestIndexLockObserver_Seen(t *testing.T) {
	t.Run("ExactMatch", func(t *testing.T) {
		observer := new(indexLockObserver)

		_, err := observer.Write([]byte("index.lock"))
		require.NoError(t, err)
		assert.True(t, observer.Seen())
	})

	t.Run("AcrossWriteBoundaries", func(t *testing.T) {
		for i := 1; i < len(_indexLockToken); i++ {
			t.Run(_indexLockToken[:i]+"|"+_indexLockToken[i:], func(t *testing.T) {
				observer := new(indexLockObserver)

				_, err := observer.Write([]byte(_indexLockToken[:i]))
				require.NoError(t, err)
				assert.False(t, observer.Seen())

				_, err = observer.Write([]byte(_indexLockToken[i:]))
				require.NoError(t, err)
				assert.True(t, observer.Seen())
			})
		}
	})

	t.Run("MismatchResetsPartialMatch", func(t *testing.T) {
		observer := new(indexLockObserver)

		_, err := observer.Write([]byte("indexXlock"))
		require.NoError(t, err)
		assert.False(t, observer.Seen())

		_, err = observer.Write([]byte(" index.lock"))
		require.NoError(t, err)
		assert.True(t, observer.Seen())
	})

	t.Run("MatchStaysLatched", func(t *testing.T) {
		observer := new(indexLockObserver)

		_, err := observer.Write([]byte("index.lock trailing text"))
		require.NoError(t, err)
		assert.True(t, observer.Seen())

		_, err = observer.Write([]byte(" more bytes"))
		require.NoError(t, err)
		assert.True(t, observer.Seen())
	})
}

func TestWorktree_runGitWithIndexLockRetry(t *testing.T) {
	t.Run("RetryDisabledBuildsOnce", func(t *testing.T) {
		oldTimeout := indexLockTimeout()
		SetIndexLockTimeout(0)
		t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

		mockExecer := NewMockExecer(gomock.NewController(t))
		_, wt := NewFakeRepository(t, "", mockExecer)

		builds := 0
		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(*exec.Cmd) error {
				return nil
			})

		err := wt.runGitWithIndexLockRetry(t.Context(), func() *gitCmd {
			builds++
			return wt.gitCmd(t.Context(), "status")
		})

		require.NoError(t, err)
		assert.Equal(t, 1, builds)
	})

	t.Run("RetryableFailureRebuildsCommand", func(t *testing.T) {
		oldTimeout := indexLockTimeout()
		SetIndexLockTimeout(time.Second)
		t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

		mockExecer := NewMockExecer(gomock.NewController(t))
		_, wt := NewFakeRepository(t, "", mockExecer)

		builds := 0
		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				if builds < 3 {
					_, _ = io.WriteString(cmd.Stderr, "fatal: index.lock\n")
					return &exec.ExitError{}
				}
				return nil
			}).
			Times(3)

		err := wt.runGitWithIndexLockRetry(t.Context(), func() *gitCmd {
			builds++
			return wt.gitCmd(t.Context(), "status")
		})

		require.NoError(t, err)
		assert.Equal(t, 3, builds)
	})

	t.Run("TerminalFailureStopsImmediately", func(t *testing.T) {
		oldTimeout := indexLockTimeout()
		SetIndexLockTimeout(time.Second)
		t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

		mockExecer := NewMockExecer(gomock.NewController(t))
		_, wt := NewFakeRepository(t, "", mockExecer)

		builds := 0
		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				_, _ = io.WriteString(cmd.Stderr, "fatal: unrelated\n")
				return &exec.ExitError{}
			})

		err := wt.runGitWithIndexLockRetry(t.Context(), func() *gitCmd {
			builds++
			return wt.gitCmd(t.Context(), "status")
		})

		require.Error(t, err)
		assert.Equal(t, 1, builds)

		var exhausted *retry.ExhaustedError
		assert.False(t, errors.As(err, &exhausted))
	})

	t.Run("TimeoutExhaustion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			oldTimeout := indexLockTimeout()
			SetIndexLockTimeout(150 * time.Millisecond)
			t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

			mockExecer := NewMockExecer(gomock.NewController(t))
			_, wt := NewFakeRepository(t, "", mockExecer)

			mockExecer.EXPECT().
				Run(gomock.Any()).
				DoAndReturn(func(cmd *exec.Cmd) error {
					_, _ = io.WriteString(cmd.Stderr, "fatal: index.lock\n")
					return &exec.ExitError{}
				}).
				AnyTimes()

			err := wt.runGitWithIndexLockRetry(t.Context(), func() *gitCmd {
				return wt.gitCmd(t.Context(), "status")
			})

			var exhausted *retry.ExhaustedError
			require.ErrorAs(t, err, &exhausted)
			assert.Equal(t, 2, exhausted.Attempts)
		})
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			oldTimeout := indexLockTimeout()
			SetIndexLockTimeout(time.Second)
			t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

			mockExecer := NewMockExecer(gomock.NewController(t))
			_, wt := NewFakeRepository(t, "", mockExecer)

			mockExecer.EXPECT().
				Run(gomock.Any()).
				DoAndReturn(func(cmd *exec.Cmd) error {
					_, _ = io.WriteString(cmd.Stderr, "fatal: index.lock\n")
					return &exec.ExitError{}
				}).
				AnyTimes()

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)

			go func() {
				done <- wt.runGitWithIndexLockRetry(ctx, func() *gitCmd {
					return wt.gitCmd(ctx, "status")
				})
			}()

			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()

			synctest.Wait()
			require.ErrorIs(t, <-done, context.Canceled)
		})
	})

	t.Run("LogsAttemptNumber", func(t *testing.T) {
		oldTimeout := indexLockTimeout()
		SetIndexLockTimeout(time.Second)
		t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

		var logBuffer bytes.Buffer
		log := silog.New(&logBuffer, &silog.Options{
			Level: silog.LevelDebug,
		})

		mockExecer := NewMockExecer(gomock.NewController(t))
		_, wt := NewFakeRepositoryWithLogger(t, "", mockExecer, log)

		attempts := 0
		mockExecer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				attempts++
				if attempts == 1 {
					_, _ = io.WriteString(cmd.Stderr, "fatal: index.lock\n")
					return &exec.ExitError{}
				}
				return nil
			}).
			Times(2)

		err := wt.runGitWithIndexLockRetry(t.Context(), func() *gitCmd {
			return wt.gitCmd(t.Context(), "status")
		})

		require.NoError(t, err)
		assert.Contains(t, logBuffer.String(),
			"Retrying Git command after index.lock contention")
		assert.Contains(t, logBuffer.String(), "attempt=1")
	})
}

func TestObserveIndexLock(t *testing.T) {
	t.Run("ObserverAttachedViaTeeStderr", func(t *testing.T) {
		cmd := &gitCmd{
			cmd: xec.Command(
				t.Context(),
				silog.Nop(),
				"sh",
				"-c",
				"echo 'fatal: index.lock' >&2; exit 1",
			),
			log: silog.Nop(),
		}
		observer := cmd.ObserveIndexLock()

		err := cmd.Run()
		require.Error(t, err)
		assert.True(t, observer.Seen())
		assert.True(t, observer.IsIndexLockErr(err))
	})

	t.Run("RequiresObservedIndexLockAndNonZeroExit", func(t *testing.T) {
		observer := new(indexLockObserver)
		_, err := observer.Write([]byte("fatal: unable to create index.lock"))
		require.NoError(t, err)

		assert.False(t, observer.IsIndexLockErr(nil))
		assert.True(t, observer.IsIndexLockErr(&exec.ExitError{}))
		assert.False(t, new(indexLockObserver).IsIndexLockErr(&exec.ExitError{}))
	})
}

func TestWorktree_WriteIndexTree_retryResetsBuffer(t *testing.T) {
	oldTimeout := indexLockTimeout()
	SetIndexLockTimeout(time.Second)
	t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

	mockExecer := NewMockExecer(gomock.NewController(t))
	_, wt := NewFakeRepository(t, "", mockExecer)

	attempts := 0
	mockExecer.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			attempts++
			if attempts == 1 {
				_, _ = io.WriteString(cmd.Stdout, "stale\n")
				_, _ = io.WriteString(cmd.Stderr, "fatal: index.lock\n")
				return &exec.ExitError{}
			}

			_, _ = io.WriteString(cmd.Stdout, "treehash\n")
			return nil
		}).
		Times(2)

	hash, err := wt.WriteIndexTree(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Hash("treehash"), hash)
}

func TestWorktree_StashCreate_retryResetsBuffer(t *testing.T) {
	oldTimeout := indexLockTimeout()
	SetIndexLockTimeout(time.Second)
	t.Cleanup(func() { SetIndexLockTimeout(oldTimeout) })

	mockExecer := NewMockExecer(gomock.NewController(t))
	_, wt := NewFakeRepository(t, "", mockExecer)

	attempts := 0
	mockExecer.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(cmd *exec.Cmd) error {
			attempts++
			if attempts == 1 {
				_, _ = io.WriteString(cmd.Stdout, "stale\n")
				_, _ = io.WriteString(cmd.Stderr, "fatal: index.lock\n")
				return &exec.ExitError{}
			}

			_, _ = io.WriteString(cmd.Stdout, "stashhash\n")
			return nil
		}).
		Times(2)

	hash, err := wt.StashCreate(t.Context(), "message")
	require.NoError(t, err)
	assert.Equal(t, Hash("stashhash"), hash)
}
