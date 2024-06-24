package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/text"
)

type downCmd struct {
	N int `arg:"" optional:"" help:"Number of branches to move up." default:"1"`

	DryRun bool `short:"n" help:"Print the target branch without checking it out."`
}

func (*downCmd) Help() string {
	return text.Dedent(`
		Checks out the branch below the current branch.
		If the current branch is at the bottom of the stack,
		checks out the trunk branch.
		Use the -n flag to print the branch without checking it out.
	`)
}

func (cmd *downCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

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

	if cmd.DryRun {
		fmt.Println(below)
		return nil
	}

	return (&branchCheckoutCmd{Branch: below}).Run(ctx, log, opts)
}
