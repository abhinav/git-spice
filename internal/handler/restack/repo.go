package restack

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/graph"
	"go.abhg.dev/gs/internal/spice"
)

// RestackRepo restacks all tracked branches in the repository in dependency order.
// This ensures that branches closer to trunk are restacked first,
// followed by their dependents.
func (h *Handler) RestackRepo(ctx context.Context) error {
	currentBranch, err := h.Worktree.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	branches, err := h.Service.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("load branches: %w", err)
	}

	if len(branches) == 0 {
		h.Log.Info("Nothing to restack: no tracked branches found")
		return nil
	}

	branchNames := make([]string, len(branches))
	branchesByName := make(map[string]spice.LoadBranchItem, len(branches))
	for idx, branch := range branches {
		branchesByName[branch.Name] = branch
		branchNames[idx] = branch.Name
	}

	// Topologically sort branches by their dependencies
	// This ensures we restack branches in the correct order:
	// branches closer to trunk first, then their dependents
	topoBranches := graph.Toposort(branchNames, func(branch string) (string, bool) {
		base := branchesByName[branch].Base
		_, ok := branchesByName[base]
		return base, ok
	})

loop:
	for _, branch := range topoBranches {
		// Trunk never needs to be restacked
		if branch == h.Store.Trunk() {
			continue loop
		}

		res, err := h.Service.Restack(ctx, branch)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return h.Service.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Branch:  currentBranch,
					Command: []string{"repo", "restack"},
					Message: fmt.Sprintf("interrupted: restack all branches (at %v)", branch),
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				h.Log.Infof("%v: branch does not need to be restacked", branch)
				continue loop
			default:
				return fmt.Errorf("restack branch %v: %w", branch, err)
			}
		}

		h.Log.Infof("%v: restacked on %v", branch, res.Base)
	}

	// On success, check out the original branch
	if err := h.Worktree.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout %v: %w", currentBranch, err)
	}

	return nil
}
