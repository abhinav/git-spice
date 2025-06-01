package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/silog/silogtest"
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
