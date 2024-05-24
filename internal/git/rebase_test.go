package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/logtest"
	"go.abhg.dev/gs/internal/mockedit"
	"go.abhg.dev/gs/internal/text"
)

func TestMain(m *testing.M) {
	testscript.RunMain(m, map[string]func() int{
		// mockedit <input>:
		"mockedit": mockedit.Main,
	})
}

func TestRebase_deliberateInterrupt(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add foo.txt
		git commit -m 'Add foo'

		git checkout -b feature

		git add bar.txt
		git commit -m 'Add bar'

		git add baz.txt
		git commit -m 'Add baz'

		-- foo.txt --
		Contents of foo

		-- bar.txt --
		Contents of bar

		-- baz.txt --
		Contents of baz
	`)))
	require.NoError(t, err)
	t.Cleanup(fixture.Cleanup)

	ctx := context.Background()
	repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
		Log: logtest.New(t),
	})
	require.NoError(t, err)

	login(t, "foo")

	// Test cases with no InterruptFunc.
	// All must see ErrRebaseInterrupted.
	noFuncTests := []struct {
		name  string
		lines []string
	}{
		{
			name: "break",
			lines: []string{
				"pick cc51432 Add bar",
				"break",
				"pick 7dd9ddf Add baz",
			},
		},
		{
			name: "edit",
			lines: []string{
				"pick cc51432 Add bar",
				"edit 7dd9ddf Add baz",
			},
		},
	}

	for _, tt := range noFuncTests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				assert.NoError(t, repo.RebaseAbort(ctx))
			}()
			mockedit.Expect(t).
				GiveLines(tt.lines...)

			err = repo.Rebase(ctx, git.RebaseRequest{
				Branch:      "feature",
				Upstream:    "main",
				Interactive: true,
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, git.ErrRebaseInterrupted)
		})
	}

	t.Run("InterruptFunc", func(t *testing.T) {
		defer func() {
			assert.NoError(t, repo.RebaseAbort(ctx))
		}()

		// Either test case will do.
		mockedit.Expect(t).
			GiveLines(noFuncTests[0].lines...)

		var calledInterrupt bool
		defer func() {
			assert.True(t, calledInterrupt, "InterruptFunc was not called")
		}()

		err = repo.Rebase(ctx, git.RebaseRequest{
			Branch:      "feature",
			Upstream:    "main",
			Interactive: true,
			InterruptFunc: func(_ context.Context, state *git.RebaseState, kind git.RebaseInterruptKind) error {
				calledInterrupt = true

				assert.Equal(t, &git.RebaseState{Branch: "feature"}, state)
				assert.Equal(t, git.RebaseInterruptDeliberate, kind)
				return nil
			},
		})
	})
}

func TestRebase_unexpectedInterrupt(t *testing.T) {
	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2024-05-21T20:30:40Z'

		git init
		git commit --allow-empty -m 'Initial commit'

		git add foo.txt
		git commit -m 'Add foo'

		git checkout -b feature
		git add bar.txt
		git commit -m 'Add bar'

		git checkout main
		mv conflicting-bar.txt bar.txt
		git add bar.txt
		git commit -m 'Conflicting bar'

		-- foo.txt --
		Contents of foo

		-- bar.txt --
		Contents of bar

		-- conflicting-bar.txt --
		Different contents of foo
	`)))
	require.NoError(t, err)

	ctx := context.Background()
	repo, err := git.Open(ctx, fixture.Dir(), git.OpenOptions{
		Log: logtest.New(t),
	})
	require.NoError(t, err)

	login(t, "user")

	t.Run("noInterruptFunc", func(t *testing.T) {
		defer func() {
			assert.NoError(t, repo.RebaseAbort(ctx))
		}()

		err = repo.Rebase(ctx, git.RebaseRequest{
			Branch:   "feature",
			Upstream: "main",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, git.ErrRebaseInterrupted)
	})

	t.Run("InterruptFunc", func(t *testing.T) {
		defer func() {
			assert.NoError(t, repo.RebaseAbort(ctx))
		}()

		var calledInterrupt bool
		defer func() {
			assert.True(t, calledInterrupt, "InterruptFunc was not called")
		}()

		err = repo.Rebase(ctx, git.RebaseRequest{
			Branch:   "feature",
			Upstream: "main",
			InterruptFunc: func(_ context.Context, state *git.RebaseState, kind git.RebaseInterruptKind) error {
				calledInterrupt = true

				assert.Equal(t, &git.RebaseState{Branch: "feature"}, state)
				assert.Equal(t, git.RebaseInterruptConflict, kind)
				return nil
			},
		})
		require.NoError(t, err)
	})
}

func login(t testing.TB, username string) (home string) {
	require.NotEmpty(t, username, "username must not be empty")
	require.NotContains(t, username, " ", "username must not contain spaces")

	home = filepath.Join(t.TempDir(), username)
	require.NoError(t, os.MkdirAll(home, 0o700))

	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_AUTHOR_NAME", username)
	t.Setenv("GIT_AUTHOR_EMAIL", username+"@example.com")
	t.Setenv("GIT_COMMITTER_NAME", username)
	t.Setenv("GIT_COMMITTER_EMAIL", username+"@example.com")
	return home
}
