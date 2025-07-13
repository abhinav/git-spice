package restack

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
)

// RestackStack restacks the stack of the given branch.
// This includes all upstack and downtrack branches,
// as well as the branch itself.
func (h *Handler) RestackStack(ctx context.Context, branch string) error {
	stack, err := h.Service.ListStack(ctx, branch)
	if err != nil {
		return fmt.Errorf("list stack: %w", err)
	}

loop:
	for _, stackBranch := range stack {
		// Trunk never needs to be restacked.
		if stackBranch == h.Store.Trunk() {
			continue loop
		}

		res, err := h.Service.Restack(ctx, stackBranch)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Command: []string{"stack", "restack"},
					Branch:  branch,
					Message: "interrupted: restack stack for " + stackBranch,
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the current branch.
				if stackBranch != branch {
					h.Log.Infof("%v: branch does not need to be restacked.", stackBranch)
				}
				continue loop
			default:
				return fmt.Errorf("restack branch: %w", err)
			}
		}

		h.Log.Infof("%v: restacked on %v", stackBranch, res.Base)
	}

	// On success, check out the original branch.
	if err := h.Worktree.Checkout(ctx, branch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", branch, err)
	}

	return nil
}
