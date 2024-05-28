package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/text"
)

type rebaseAbortCmd struct{}

func (*rebaseAbortCmd) Help() string {
	return text.Dedent(`
		This command cancels an ongoing git-spice operation that was
		interrupted by a Git rebase action.
		Without an ongoing git-spice operation,
		this is equivalent to 'git rebase --abort'.

		For example, if 'gs upstack restack' encounters a conflict,
		cancel the operation with 'gs rebase abort'
		(or its shorthand 'gs rba').
	`)
}

func (cmd *rebaseAbortCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) error {
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

	var wasRebasing bool
	if _, err := repo.RebaseState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoRebase) {
			return fmt.Errorf("get rebase state: %w", err)
		}
		// If the user ran 'git rebase --abort' instead,
		// we will not be in the middle of a rebase operation.
		// That's okay -- assume that they still want to abort
		// the gs operation they were running.
	} else {
		wasRebasing = true
		if err := repo.RebaseAbort(ctx); err != nil {
			return fmt.Errorf("abort rebase: %w", err)
		}
	}

	cont, err := store.TakeContinuation(ctx, "gs rebase abort")
	if err != nil {
		return fmt.Errorf("take rebase continuation: %w", err)
	}
	if cont == nil && !wasRebasing {
		return errors.New("no operation to abort")
	}
	if cont != nil {
		log.Debugf("%v: dropping continuation: %q", cont.Branch, cont.Command)
	}

	return nil
}
