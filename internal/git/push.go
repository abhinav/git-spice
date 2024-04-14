package git

import (
	"context"
	"errors"
	"fmt"
)

type PushOptions struct {
	// Remote is the remote to push to.
	//
	// If empty, the default remote for the current branch is used.
	// If the current branch does not have a remote configured,
	// the operation fails.
	Remote string

	ForceWithLease bool

	// Refspec is the refspec to push.
	// If empty, the current branch is pushed to the remote.
	Refspec string // TODO: Refspec type?
}

// Push pushes objects and refs to a remote repository.
func (r *Repository) Push(ctx context.Context, opts PushOptions) error {
	if opts.Remote == "" && opts.Refspec == "" {
		return errors.New("push: no remote or refspec specified")
	}

	args := []string{"push"}
	if opts.ForceWithLease {
		args = append(args, "--force-with-lease")
	}
	if opts.Remote != "" {
		args = append(args, opts.Remote)
	}
	if opts.Refspec != "" {
		args = append(args, opts.Refspec)
	}

	if err := r.gitCmd(ctx, args...).Run(r.exec); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	return nil
}
