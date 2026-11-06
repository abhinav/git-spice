package git

import (
	"cmp"
	"context"
	"fmt"
	"io"
)

// StripspaceOptions controls the behavior of Stripspace.
type StripspaceOptions struct {
	// StripComments removes lines starting
	// with the comment character.
	StripComments bool

	// CommentLines prepends the comment character
	// to each line of the input.
	CommentLines bool
}

// Stripspace processes input through git stripspace,
// writing the result to w.
//
// By default, it strips trailing whitespace
// and collapses blank lines.
// Use opts to control comment stripping or prepending.
//
// StripComments and CommentLines are mutually exclusive.
func (r *Repository) Stripspace(ctx context.Context, i io.Reader, o io.Writer, opts *StripspaceOptions) error {
	opts = cmp.Or(opts, &StripspaceOptions{})

	args := []string{"stripspace"}
	if opts.StripComments {
		args = append(args, "--strip-comments")
	}
	if opts.CommentLines {
		args = append(args, "--comment-lines")
	}

	cmd := r.gitCmd(ctx, args[0], args[1:]...).
		WithStdin(i).
		WithStdout(o)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stripspace: %w", err)
	}
	return nil
}
