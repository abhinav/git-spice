package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.abhg.dev/git-spice/internal/logtest"
)

func NewTestRepository(t testing.TB, dir string, execer execer) *Repository {
	if dir == "" {
		dir = t.TempDir()
	}
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		if !errors.Is(err, os.ErrExist) {
			t.Fatalf("failed to create .git directory: %v", err)
		}
	}

	return newRepository(dir, gitDir, logtest.New(t), execer)
}
