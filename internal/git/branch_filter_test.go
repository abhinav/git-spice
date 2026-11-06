package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/sliceutil"
)

func TestLocalBranches_filterFallback(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(`
as 'Test <test@example.com>'
git init
git commit --allow-empty -m initial
`))
	require.NoError(t, err)
	repo, err := Open(t.Context(), fixture.Dir(), OpenOptions{Log: silogtest.New(t)})
	require.NoError(t, err)
	for _, name := range []string{"topic/a", "topic/deep/b", "topic2", "fix1", "fix2", "fixd", "unicode/λ"} {
		require.NoError(t, repo.CreateBranch(t.Context(), CreateBranchRequest{Name: name, Head: "HEAD"}))
	}

	patterns := [][]string{
		{""},
		{"", "absent"},
		{"main", "fix2", "main"},
		{"absent", "fix1"},
		{"topic"},
		{"topic/"},
		{"topic", "topic/", "fix1"},
		{"topic/deep"},
		{"topic/deep/"},
		{"top"},
		{"unicode/"},
		{"unicode/λ"},
	}
	for _, patterns := range patterns {
		t.Run(fmt.Sprint(patterns), func(t *testing.T) {
			opts := &LocalBranchesOptions{Patterns: patterns, Sort: "-refname"}
			want, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), opts))
			require.NoError(t, err)
			limited := &limitedRefExecer{execer: _realExec, err: syscall.E2BIG}
			fallback, err := Open(t.Context(), fixture.Dir(), OpenOptions{
				Log: silogtest.New(t), exec: limited,
			})
			require.NoError(t, err)

			got, err := sliceutil.CollectErr(fallback.LocalBranches(t.Context(), opts))
			require.NoError(t, err)
			assert.Equal(t, want, got)
			assert.Len(t, limited.calls, 2)
			assert.Contains(t, limited.calls[1], "refs/heads/")
		})
	}

	t.Run("NativeArgumentLimit", func(t *testing.T) {
		want, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &LocalBranchesOptions{
			Patterns: []string{"main"},
		}))
		require.NoError(t, err)
		got, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &LocalBranchesOptions{
			Patterns: []string{strings.Repeat("x", 8*1024*1024), "main"},
		}))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("ConsumerStopsFallback", func(t *testing.T) {
		limited := &limitedRefExecer{execer: _realExec, err: syscall.E2BIG}
		fallback, err := Open(t.Context(), fixture.Dir(), OpenOptions{
			Log: silogtest.New(t), exec: limited,
		})
		require.NoError(t, err)
		var count int
		for _, err := range fallback.LocalBranches(t.Context(), &LocalBranchesOptions{Patterns: []string{""}}) {
			require.NoError(t, err)
			count++
			break
		}
		assert.Equal(t, 1, count)
		assert.Len(t, limited.calls, 2)
	})

	t.Run("FailureAfterOutputDoesNotRetry", func(t *testing.T) {
		limited := &limitedRefExecer{execer: _realExec, waitErr: syscall.E2BIG}
		fallback, err := Open(t.Context(), fixture.Dir(), OpenOptions{
			Log: silogtest.New(t), exec: limited,
		})
		require.NoError(t, err)
		_, err = sliceutil.CollectErr(fallback.LocalBranches(t.Context(), &LocalBranchesOptions{
			Patterns: []string{"main"},
		}))
		assert.ErrorIs(t, err, syscall.E2BIG)
		assert.Len(t, limited.calls, 1)
	})

	t.Run("FallbackFailureDoesNotRetry", func(t *testing.T) {
		limited := &limitedRefExecer{execer: _realExec, err: syscall.E2BIG, waitErr: syscall.E2BIG}
		fallback, err := Open(t.Context(), fixture.Dir(), OpenOptions{
			Log: silogtest.New(t), exec: limited,
		})
		require.NoError(t, err)
		branches, err := sliceutil.CollectErr(fallback.LocalBranches(t.Context(), &LocalBranchesOptions{
			Patterns: []string{"absent"},
		}))
		assert.ErrorIs(t, err, syscall.E2BIG)
		assert.Empty(t, branches)
		assert.Len(t, limited.calls, 2)
	})
}

func TestLocalBranches_filterFailure(t *testing.T) {
	for _, err := range []error{os.ErrPermission, errors.New("unrelated start failure")} {
		t.Run(err.Error(), func(t *testing.T) {
			limited := &limitedRefExecer{execer: _realExec, err: err}
			repo, _ := NewFakeRepository(t, "", limited)
			_, gotErr := sliceutil.CollectErr(repo.LocalBranches(t.Context(), &LocalBranchesOptions{
				Patterns: []string{"main"},
			}))
			assert.ErrorIs(t, gotErr, err)
			assert.Len(t, limited.calls, 1)
		})
	}

	t.Run("UnfilteredDoesNotRetry", func(t *testing.T) {
		limited := &limitedRefExecer{execer: _realExec, err: syscall.E2BIG}
		repo, _ := NewFakeRepository(t, "", limited)
		_, err := sliceutil.CollectErr(repo.LocalBranches(t.Context(), nil))
		assert.ErrorIs(t, err, syscall.E2BIG)
		assert.Len(t, limited.calls, 1)
	})
}

type limitedRefExecer struct {
	execer
	err     error
	waitErr error
	calls   [][]string
}

func (e *limitedRefExecer) Start(cmd *exec.Cmd) error {
	if slices.Contains(cmd.Args, "for-each-ref") {
		e.calls = append(e.calls, slices.Clone(cmd.Args))
		if len(e.calls) == 1 && e.err != nil {
			return fmt.Errorf("launch Git: %w", &os.PathError{Op: "fork/exec", Path: cmd.Path, Err: e.err})
		}
	}
	return e.execer.Start(cmd)
}

func (e *limitedRefExecer) Wait(cmd *exec.Cmd) error {
	if err := e.execer.Wait(cmd); err != nil {
		return err
	}
	return e.waitErr
}
