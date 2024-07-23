package main

import (
	"cmp"
	"context"
	"os"

	"go.abhg.dev/gs/internal/git"
)

// gitEditor returns the editor to use
// to prompt the user to fill information.
func gitEditor(ctx context.Context, repo *git.Repository) string {
	gitEditor, err := repo.Var(ctx, "GIT_EDITOR")
	if err != nil {
		// 'git var GIT_EDITOR' will basically never fail,
		// but if it does, fall back to EDITOR or vi.
		return cmp.Or(os.Getenv("EDITOR"), "vi")
	}
	return gitEditor
}
