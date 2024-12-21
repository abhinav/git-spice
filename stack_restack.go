package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type stackRestackCmd struct{}

func (*stackRestackCmd) Help() string {
	return text.Dedent(`
		All branches in the current stack are rebased on top of their
		respective bases, ensuring a linear history.
	`)
}

func (*stackRestackCmd) Run(
	ctx context.Context,
	log *log.Logger,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	stack, err := svc.ListStack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("list stack: %w", err)
	}

loop:
	for _, branch := range stack {
		// Trunk never needs to be restacked.
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
					Command: []string{"stack", "restack"},
					Branch:  currentBranch,
					Message: fmt.Sprintf("interrupted: restack stack for %s", branch),
				})
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the current branch.
				if branch != currentBranch {
					log.Infof("%v: branch does not need to be restacked.", branch)
				}
				continue loop
			default:
				return fmt.Errorf("restack branch: %w", err)
			}
		}

		log.Infof("%v: restacked on %v", branch, res.Base)
	}

	// On success, check out the original branch.
	if err := repo.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", currentBranch, err)
	}

	return nil
}
