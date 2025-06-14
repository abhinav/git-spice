package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ErrNotExist is returned when a Git object does not exist.
var ErrNotExist = errors.New("does not exist")

// Hash is a 40-character Git object ID.
type Hash string

// ZeroHash is the hash of an empty Git object.
// It is used to represent the absence of a hash.
const ZeroHash Hash = "0000000000000000000000000000000000000000"

func (h Hash) String() string {
	return string(h)
}

// LogValue reports how the hash should be logged.
func (h Hash) LogValue() slog.Value {
	return slog.StringValue(h.Short())
}

// Short reports the short form of the hash.
func (h Hash) Short() string {
	if len(h) < 7 {
		return string(h)
	}
	return string(h[:7])
}

// IsZero reports whether the hash is the zero hash.
func (h Hash) IsZero() bool {
	// We're not just comparing to ZeroHash
	// to make this also work with abbreviated hashes.
	for _, b := range h {
		if b != '0' {
			return false
		}
	}
	return true
}

// PeelToCommit reports the commit hash of the provided commit-ish.
// It returns [ErrNotExist] if the object does not exist.
func (r *Repository) PeelToCommit(ctx context.Context, ref string) (Hash, error) {
	return r.revParse(ctx, ref+"^{commit}")
}

// PeelToTree reports the tree object at the provided tree-ish.
// It returns [ErrNotExist] if the object does not exist.
func (r *Repository) PeelToTree(ctx context.Context, ref string) (Hash, error) {
	return r.revParse(ctx, ref+"^{tree}")
}

// HashAt reports the hash of the object at the provided path in the given
// treeish.
func (r *Repository) HashAt(ctx context.Context, treeish, path string) (Hash, error) {
	return r.revParse(ctx, treeish+":"+path)
}

// ForkPoint reports the point at which b diverged from a.
// See man git-merge-base for more information.
func (r *Repository) ForkPoint(ctx context.Context, a, b string) (Hash, error) {
	s, err := r.gitCmd(ctx, "merge-base", "--fork-point", a, b).OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("merge-base: %w", err)
	}
	return Hash(s), nil
}

// MergeBase reports the common ancestor of a and b.
func (r *Repository) MergeBase(ctx context.Context, a, b string) (Hash, error) {
	s, err := r.gitCmd(ctx, "merge-base", a, b).OutputString(r.exec)
	if err != nil {
		return "", fmt.Errorf("merge-base: %w", err)
	}
	return Hash(s), nil
}

// IsAncestor reports whether a is an ancestor of b.
func (r *Repository) IsAncestor(ctx context.Context, a, b Hash) bool {
	return r.gitCmd(ctx,
		"merge-base", "--is-ancestor", string(a), string(b),
	).Run(r.exec) == nil
}

func (r *Repository) revParse(ctx context.Context, ref string) (Hash, error) {
	out, err := r.revParseCmd(ctx, ref).OutputString(r.exec)
	if err != nil {
		return "", ErrNotExist
	}
	return Hash(out), nil
}

func (r *Repository) revParseCmd(ctx context.Context, ref string) *gitCmd {
	return r.gitCmd(ctx, "rev-parse",
		"--verify",         // fail if the object does not exist
		"--quiet",          // no output if object does not exist
		"--end-of-options", // prevent ref from being treated as a flag
		ref,
	)
}
