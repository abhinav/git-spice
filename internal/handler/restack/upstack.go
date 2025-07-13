package restack

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
)

// UpstackOptions holds options for restacking the upstack of a branch.
type UpstackOptions struct {
	// SkipStart indicates that the starting branch should not be restacked.
	SkipStart bool `help:"Do not restack the starting branch"`
}

// RestackUpstack restacks the upstack of the given branch,
// including the branch itself, unless SkipStart is set.
func (h *Handler) RestackUpstack(ctx context.Context, branch string, opts *UpstackOptions) error {
	log := h.Log
	opts = cmp.Or(opts, &UpstackOptions{})
	upstacks, err := h.Service.ListUpstack(ctx, branch)
	if err != nil {
		return fmt.Errorf("get upstack branches: %w", err)
	}
	if opts.SkipStart && len(upstacks) > 0 && upstacks[0] == branch {
		upstacks = upstacks[1:]
		if len(upstacks) == 0 {
			return nil
		}
	}

loop:
	for _, upstack := range upstacks {
		// Trunk never needs to be restacked.
		if upstack == h.Store.Trunk() {
			continue loop
		}

		res, err := h.Service.Restack(ctx, upstack)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Command: []string{"upstack", "restack"},
					Branch:  branch,
					Message: fmt.Sprintf("interrupted: restack upstack of %v", branch),
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the base branch.
				if upstack != branch {
					log.Infof("%v: branch does not need to be restacked.", upstack)
				}
				continue loop
			default:
				return fmt.Errorf("restack branch: %w", err)
			}
		}

		log.Infof("%v: restacked on %v", upstack, res.Base)
	}

	// On success, check out the original branch.
	if err := h.Worktree.Checkout(ctx, branch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", branch, err)
	}

	return nil
}
