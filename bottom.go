package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/text"
)

type bottomCmd struct {
	DryRun bool `short:"n" help:"Print the target branch without checking it out."`
}

func (*bottomCmd) Help() string {
	return text.Dedent(`
		Jumps to the bottom-most branch below the current branch.
		This is the branch just above the trunk.
	`)
}

func (cmd *bottomCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, store, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	if current == store.Trunk() {
		return errors.New("no branches below current: already on trunk")
	}

	bottom, err := svc.FindBottom(ctx, current)
	if err != nil {
		return fmt.Errorf("find bottom: %w", err)
	}

	if cmd.DryRun {
		fmt.Println(bottom)
		return nil
	}

	return (&branchCheckoutCmd{Name: bottom}).Run(ctx, log, opts)
}
