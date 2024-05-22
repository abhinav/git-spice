package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchRestackCmd struct {
	Name string `arg:"" optional:"" help:"Branch to restack" predictor:"trackedBranches"`
}

func (*branchRestackCmd) Help() string {
	return text.Dedent(`
		Updates a branch after its base branch has been changed,
		rebasing its commits on top of the base.
	`)
}

func (cmd *branchRestackCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	if cmd.Name == "" {
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Name = currentBranch
	}

	svc := spice.NewService(repo, store, log)
	res, err := svc.Restack(ctx, cmd.Name)
	if err != nil {
		var rebaseErr *git.RebaseInterruptError
		switch {
		case errors.As(err, &rebaseErr):
			// If the rebase is interrupted by a conflict,
			// we'll resume by re-running this command.
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err:     rebaseErr,
				Command: []string{"branch", "restack", cmd.Name},
				Branch:  cmd.Name,
				Message: fmt.Sprintf("interrupted: restack branch %s", cmd.Name),
			})
		case errors.Is(err, state.ErrNotExist):
			log.Errorf("%v: branch not tracked: run 'gs branch track'", cmd.Name)
			return errors.New("untracked branch")
		case errors.Is(err, spice.ErrAlreadyRestacked):
			log.Infof("%v: branch does not need to be restacked.", cmd.Name)
			return nil
		}
		return fmt.Errorf("restack branch: %w", err)
	}

	log.Infof("%v: restacked on %v", cmd.Name, res.Base)
	return nil
}
