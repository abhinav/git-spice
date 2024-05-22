package gitspice

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
	"go.abhg.dev/git-spice/internal/spice"
	"go.abhg.dev/git-spice/internal/text"
)

type upstackRestackCmd struct{}

func (*upstackRestackCmd) Help() string {
	return text.Dedent(`
		Restacks the current branch and all branches above it
		on top of their respective bases.
		If multiple branches use another branch as their base,
		they will all be restacked on top of the updated base.

		Run this command from the trunk branch
		to restack all managed branches.
	`)
}

func (*upstackRestackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	svc := spice.NewService(repo, store, log)

	upstacks, err := svc.ListUpstack(ctx, currentBranch)
	if err != nil {
		return fmt.Errorf("get upstack branches: %w", err)
	}

loop:
	for _, upstack := range upstacks {
		// Trunk never needs to be restacked.
		if upstack == store.Trunk() {
			continue loop
		}

		res, err := svc.Restack(ctx, upstack)
		if err != nil {
			switch {
			case errors.Is(err, spice.ErrAlreadyRestacked):
				// Log the "does not need to be restacked" message
				// only for branches that are not the current branch.
				if upstack != currentBranch {
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
	if err := repo.Checkout(ctx, currentBranch); err != nil {
		return fmt.Errorf("checkout branch %v: %w", currentBranch, err)
	}

	return nil
}
