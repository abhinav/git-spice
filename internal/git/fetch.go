package git

import (
	"context"
	"fmt"
)

type FetchOptions struct {
	Remote   string
	Refspecs []string // TODO: Refspec type?
}

func (r *Repository) Fetch(ctx context.Context, opts FetchOptions) error {
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
