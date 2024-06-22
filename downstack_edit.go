package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type downstackEditCmd struct {
	Editor string `env:"EDITOR" help:"Editor to use for editing the downstack."`

	Name string `arg:"" optional:"" help:"Name of the branch to start editing from." predictor:"trackedBranches"`
}

func (*downstackEditCmd) Help() string {
	return text.Dedent(`
		Opens an editor to allow changing the order of branches
		from trunk to the current branch.
		The branch at the top of the list will be checked out
		as the topmost branch in the downstack.
		Branches upstack of the current branch will not be modified.
		Branches deleted from the list will also not be modified.
	`)
}

func (cmd *downstackEditCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Editor == "" {
		return errors.New("an editor is required: use --editor or set $EDITOR")
	}

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	if cmd.Name == store.Trunk() {
		return errors.New("cannot edit below trunk")
	}

	downstacks, err := svc.ListDownstack(ctx, cmd.Name)
	if err != nil {
		return fmt.Errorf("list downstack: %w", err)
	}
	must.NotBeEmptyf(downstacks, "downstack cannot be empty")
	must.BeEqualf(downstacks[0], cmd.Name,
		"downstack must start with the original branch")

	if len(downstacks) == 1 {
		log.Infof("nothing to edit below %s", cmd.Name)
		return nil
	}

	slices.Reverse(downstacks) // branch closest to trunk first
	res, err := svc.StackEdit(ctx, &spice.StackEditRequest{
		Stack:  downstacks,
		Editor: cmd.Editor,
	})
	if err != nil {
		if errors.Is(err, spice.ErrStackEditAborted) {
			log.Infof("downstack edit aborted")
			return nil
		}

		// TODO: we can probably recover from the rebase operation
		// by saving the branch list somewhere,
		// and allowing it to be provided as input to the command.
		return fmt.Errorf("edit downstack: %w", err)
	}

	return (&branchCheckoutCmd{
		Name: res.Stack[len(res.Stack)-1],
	}).Run(ctx, log, opts)
}
