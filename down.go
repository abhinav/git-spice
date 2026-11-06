package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type downCmd struct {
	checkout.Options

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
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	checkoutHandler CheckoutHandler,
) error {
	if cmd.N <= 0 {
		return errors.New("number of branches must be positive")
	}

	current, _, err := currentBranchForNavigation(ctx, wt)
	if err != nil {
		return err
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("load branch graph: %w", err)
	}

	downstack := slices.Collect(graph.Downstack(current))
	if len(downstack) == 0 {
		return fmt.Errorf("%v: no branches found downstack", current)
	}

	var below string
	if cmd.N >= len(downstack) {
		log.Info("moving to trunk: end of stack")
		below = store.Trunk()
	} else {
		below = downstack[cmd.N]
	}

	return checkoutHandler.CheckoutBranch(ctx, &checkout.Request{
		Branch:  below,
		Options: &cmd.Options,
	})
}
