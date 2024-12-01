package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/logtest"
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
