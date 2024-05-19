package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type downCmd struct {
	// TODO: arg for number of branches to move down
}

func (*downCmd) Help() string {
	return text.Dedent(`
		Moves down the stack to the branch below the current branch.
		As a convenience,
		if the current branch is at the bottom of the stack,
		this command will move to the trunk branch.
	`)
}

func (*downCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	svc := spice.NewService(repo, store, log)

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	downstack, err := svc.ListDownstack(ctx, current)
	if err != nil {
		return fmt.Errorf("list downstacks: %w", err)
	}

	var below string
	switch len(downstack) {
	case 0:
		return fmt.Errorf("%v: no branches found downstack", current)

	case 1:
		// Current branch is bottom of stack.
		// Move to trunk.
		log.Info("moving to trunk: end of stack")
		below = store.Trunk()

	default:
		below = downstack[1]
	}

	return (&branchCheckoutCmd{Name: below}).Run(ctx, log, opts)
}
