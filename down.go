package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/log"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type downCmd struct {
	checkoutOptions

	N int `arg:"" optional:"" help:"Number of branches to move up." default:"1"`
}

func (*downCmd) Help() string {
	return text.Dedent(`
		Checks out the branch below the current branch.
		If the current branch is at the bottom of the stack,
		checks out the trunk branch.
		Use the -n flag to print the branch without checking it out.
	`)
}

func (cmd *downCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	var below string
outer:
	for range cmd.N {
		downstack, err := svc.ListDownstack(ctx, current)
		if err != nil {
			return fmt.Errorf("list downstacks: %w", err)
		}

		switch len(downstack) {
		case 0:
			if below != "" {
				// If we've already moved up once,
				// and there are no branches above the current one,
				// we're done.
				break outer
			}
			return fmt.Errorf("%v: no branches found downstack", current)

		case 1:
			// Current branch is bottom of stack.
			// Move to trunk.
			log.Info("moving to trunk: end of stack")
			below = store.Trunk()

		default:
			below = downstack[1]
		}

		current = below
	}

	return (&branchCheckoutCmd{
		checkoutOptions: cmd.checkoutOptions,
		Branch:          below,
	}).Run(ctx, log, view, repo, store, svc)
}
