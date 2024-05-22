package gitspice

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/git-spice/internal/git"
	"go.abhg.dev/git-spice/internal/spice"
	"go.abhg.dev/git-spice/internal/state"
)

type branchCheckoutCmd struct {
	Name string `arg:"" optional:"" help:"Name of the branch to delete" predictor:"branches"`
}

func (cmd *branchCheckoutCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	// TODO: prompt for branch if not provided or not an exact match
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	if err := svc.VerifyRestacked(ctx, cmd.Name); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		switch {
		case errors.As(err, &restackErr):
			log.Warnf("%v: needs to be restacked: run 'gs branch restack %v'", cmd.Name, cmd.Name)
		case errors.Is(err, state.ErrNotExist):
			// TODO: in interactive mode, prompt to track.
			if store.Trunk() != cmd.Name {
				log.Warnf("%v: branch not tracked: run 'gs branch track'", cmd.Name)
			}
		case errors.Is(err, git.ErrNotExist):
			return fmt.Errorf("branch %q does not exist", cmd.Name)
		default:
			log.Warnf("error checking branch: %v", err)
		}
	}

	if err := repo.Checkout(ctx, cmd.Name); err != nil {
		return fmt.Errorf("checkout %q: %w", cmd.Name, err)
	}

	return nil
}
