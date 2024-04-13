package git

import (
	"context"
	"errors"
	"fmt"
)

// FetchOptions specifies parameters for the Fetch method.
type FetchOptions struct {
	// Remote is the remote to fetch from.
	//
	// If empty, the default remote for the current branch is used.
	// If the current branch does not have a remote configured,
	// the operation fails.
	Remote string

	// Refspecs are the refspecs to fetch.
	// If non-empty, the Remote must be specified as well.
	Refspecs []string // TODO: Refspec type?
}

// Fetch fetches objects and refs from a remote repository.
func (r *Repository) Fetch(ctx context.Context, opts FetchOptions) error {
	if opts.Remote == "" && len(opts.Refspecs) == 0 {
		return errors.New("fetch: no remote or refspecs specified")
	}

	args := []string{"fetch"}
	if opts.Remote != "" {
		args = append(args, opts.Remote)
	}
	args = append(args, opts.Refspecs...)

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	return nil
}
