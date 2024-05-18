package git

import (
	"testing"

	"go.abhg.dev/gs/internal/logtest"
)

func NewTestRepository(t testing.TB, dir string, execer execer) *Repository {
	if dir == "" {
		dir = t.TempDir()
	}
	return newRepository(dir, logtest.New(t), execer)
}
