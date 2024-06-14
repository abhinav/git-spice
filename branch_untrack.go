package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchUntrackCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch to untrack" predictor:"branches"`
}

func (*branchUntrackCmd) Help() string {
	return text.Dedent(`
		Removes information about a tracked branch,
		without deleting the branch itself.
		If the branch has any branches upstack from it,
		they will be updated to point to its base branch.
	`)
}

func (cmd *branchUntrackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, _, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	if cmd.Name == "" {
		cmd.Name, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	// TODO: prompt for confirmation?
	if err := svc.ForgetBranch(ctx, cmd.Name); err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return errors.New("branch not tracked")
		}

		return fmt.Errorf("forget branch: %w", err)
	}

	return nil
}
