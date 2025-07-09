package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/graph"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type repoRestackCmd struct{}

func (*repoRestackCmd) Help() string {
	return text.Dedent(`
		All tracked branches in the repository are rebased on top of their
		respective bases in dependency order, ensuring a linear history.
	`)
}

func (*repoRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
) error {
	currentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	branches, err := svc.LoadBranches(ctx)
	if err != nil {
		return fmt.Errorf("load branches: %w", err)
	}

	if len(branches) == 0 {
		log.Info("Nothing to restack: no tracked branches found")
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
		if branch == store.Trunk() {
			continue loop
		}

		res, err := svc.Restack(ctx, branch)
		if err != nil {
			var rebaseErr *git.RebaseInterruptError
			switch {
			case errors.As(err, &rebaseErr):
				// If the rebase is interrupted by a conflict,
				// we'll resume by re-running this command.
				return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
					Err:     rebaseErr,
					Branch:  currentBranch,
					Command: []string{"repo", "restack"},
					Message: fmt.Sprintf("interrupted: restack all branches (at %v)", branch),
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				log.Infof("%v: branch does not need to be restacked", branch)
				continue loop
			default:
				return fmt.Errorf("restack branch %v: %w", branch, err)
			}
		}

		log.Infof("%v: restacked on %v", branch, res.Base)
	}

	// On success, check out the original branch
	if err := wt.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout %v: %w", currentBranch, err)
	}

	return nil
}
