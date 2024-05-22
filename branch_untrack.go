package gitspice

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
	"go.abhg.dev/git-spice/internal/spice"
	"go.abhg.dev/git-spice/internal/spice/state"
	"go.abhg.dev/git-spice/internal/text"
)

type branchUntrackCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch to untrack" predictor:"branches"`
}

func (*branchUntrackCmd) Help() string {
	return text.Dedent(`
		Removes information about a tracked branch from git-spice.
		Use this to forget about branches that were deleted outside
		git-spice, or those that are no longer relevant.
	`)
}

func (cmd *branchUntrackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	if cmd.Name == "" {
		cmd.Name, err = repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := spice.NewService(repo, store, log)

	// TODO: prompt for confirmation?
	if err := svc.ForgetBranch(ctx, cmd.Name); err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return errors.New("branch not tracked")
		}

		return fmt.Errorf("forget branch: %w", err)
	}

	return nil
}
