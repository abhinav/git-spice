package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/git/gittest"
	"go.abhg.dev/gs/internal/silog/silogtest"
	"go.abhg.dev/gs/internal/text"
)

func NewFakeRepository(t testing.TB, dir string, execer execer) (*Repository, *Worktree) {
	if dir == "" {
		dir = t.TempDir()
	}
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		if !errors.Is(err, os.ErrExist) {
			t.Fatalf("failed to create .git directory: %v", err)
		}
	}

	repo := newRepository(gitDir, silogtest.New(t), execer)
	wt := newWorktree(gitDir, dir, repo, silogtest.New(t), execer)
	return repo, wt
}

func TestOpenWorktree_correctGitDirectory(t *testing.T) {
	// This test verifies the fix for the worktree bug where OpenWorktree
	// was using the wrong git directory when constructing Repository objects.
	t.Parallel()

	fixture, err := gittest.LoadFixtureScript([]byte(text.Dedent(`
		as 'Test <test@example.com>'
		at '2025-06-26T21:28:29Z'

		mkdir repo
		cd repo
		git init
		git add main.txt
		git commit -m 'Initial commit'

		# Create a worktree
		git worktree add ../worktree -b feature

		-- repo/main.txt --
		main content
	`)))
	require.NoError(t, err)
	dir := fixture.Dir()

	ctx := t.Context()
	mainWt, err := OpenWorktree(ctx, filepath.Join(dir, "repo"), OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	wt, err := OpenWorktree(ctx, filepath.Join(dir, "worktree"), OpenOptions{
		Log: silogtest.New(t),
	})
	require.NoError(t, err)

	// Both should reference the same repository git directory
	// This ensures the fix where gitCommonDir is used instead of gitDir
	assert.Equal(t, mainWt.Repository().gitDir, wt.Repository().gitDir,
		"repositories for both worktrees should share the same git directory")

	assert.NotEqual(t, mainWt.gitDir, wt.gitDir,
		"worktrees should have different git directories")
}

func TestExtraConfig_Args(t *testing.T) {
	tests := []struct {
		name string
		give extraConfig
		want []string
	}{
		{name: "empty"},
		{
			name: "editor",
			give: extraConfig{Editor: "vim"},
			want: []string{"-c", "core.editor=vim"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.give.Args()
			assert.Equal(t, tt.want, got)
		})
	}
}
