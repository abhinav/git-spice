package git

import (
	"context"

	"go.abhg.dev/gs/internal/xec"
)

// Head reports the commit hash of HEAD.
func (w *Worktree) Head(ctx context.Context) (Hash, error) {
	return w.PeelToCommit(ctx, "HEAD")
}

// PeelToCommit reports the commit hash of the provided commit-ish
// in the context of the worktree (e.g. HEAD refers to the worktree's HEAD).
// It returns [ErrNotExist] if the object does not exist.
func (w *Worktree) PeelToCommit(ctx context.Context, ref string) (Hash, error) {
	return w.revParse(ctx, ref+"^{commit}")
}

func (w *Worktree) revParse(ctx context.Context, ref string) (Hash, error) {
	out, err := w.revParseCmd(ctx, ref).OutputChomp()
	if err != nil {
		return "", ErrNotExist
	}
	return Hash(out), nil
}

func (w *Worktree) revParseCmd(ctx context.Context, ref string) *xec.Cmd {
	return w.repo.revParseCmd(ctx, ref).WithDir(w.rootDir)
}
