package restack

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
)

// RestackBranch restacks the given branch onto its base.
func (h *Handler) RestackBranch(ctx context.Context, branch string) error {
	res, err := h.Service.Restack(ctx, branch)
	if err != nil {
		var rebaseErr *git.RebaseInterruptError
		switch {
		case errors.As(err, &rebaseErr):
			// If the rebase is interrupted by a conflict,
			// we'll resume by re-running this command.
			return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     rebaseErr,
				Command: []string{"branch", "restack"},
				Branch:  branch,
				Message: "interrupted: restack branch " + branch,
			})
		case errors.Is(err, state.ErrNotExist):
			h.Log.Errorf("%v: branch not tracked: run 'gs branch track'", branch)
			return errors.New("untracked branch")
		case errors.Is(err, spice.ErrAlreadyRestacked):
			h.Log.Infof("%v: branch does not need to be restacked.", branch)
			return nil
		}
		return fmt.Errorf("restack branch: %w", err)
	}

	h.Log.Infof("%v: restacked on %v", branch, res.Base)
	return nil
}
