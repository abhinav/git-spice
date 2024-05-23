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

type branchEditCmd struct{}

func (*branchEditCmd) Help() string {
	return text.Dedent(`
		Allows editing the commits in the current branch
		with an interactive rebase.
	`)
}

func (*branchEditCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	currentBranch, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	b, err := svc.LookupBranch(ctx, currentBranch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf("branch not tracked: %s", currentBranch)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	if err := repo.Rebase(ctx, git.RebaseRequest{
		Interactive: true,
		Branch:      currentBranch,
		Upstream:    b.Base,
	}); err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	// TODO: if, when rebase returns, we're in the middle of a rebase,
	// print a message informing the user that they should run
	// `gs continue` after they've finished the rebase operation.

	return (&upstackRestackCmd{}).Run(ctx, log, opts)
}
