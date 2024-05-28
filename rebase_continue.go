package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type rebaseContinueCmd struct{}

func (*rebaseContinueCmd) Help() string {
	return text.Dedent(`
		This command continues an ongoing git-spice operation that was
		interrupted by a Git rebase action.
		Without an ongoing git-spice operation,
		this is equivalent to 'git rebase --continue'.

		For example, if 'gs upstack restack' encounters a conflict,
		resolve the conflict and run 'gs rebase continue'
		(or its shorthand 'gs rbc') to continue the operation.
	`)
}

func (cmd *rebaseContinueCmd) Run(
	ctx context.Context,
	log *log.Logger,
	opts *globalOptions,
	parser *kong.Kong,
) error {
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

	var wasRebasing bool
	if _, err := repo.RebaseState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoRebase) {
			return fmt.Errorf("get rebase state: %w", err)
		}
		// If the user ran 'git rebase --continue' instead,
		// we will not be in the middle of a rebase operation.
		// That's okay -- assume that they still want to continue
		// with the gs operations they were running.
	} else {
		// If we're in the middle of a rebase, finish it.
		wasRebasing = true
		if err := repo.RebaseContinue(ctx); err != nil {
			return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
				Err: err,
			})
		}
	}

	cont, err := store.TakeContinuation(ctx, "gs rebase continue")
	if err != nil {
		return fmt.Errorf("take rebase continuation: %w", err)
	}
	if cont == nil && !wasRebasing {
		return errors.New("no operation to continue")
	}
	for cont != nil {
		log.Debugf("Got rebase continuation: %q (branch: %s)", cont.Command, cont.Branch)
		if err := repo.Checkout(ctx, cont.Branch); err != nil {
			return fmt.Errorf("checkout branch %q: %w", cont.Branch, err)
		}

		kctx, err := parser.Parse(cont.Command)
		if err != nil {
			log.Errorf("Corrupt rebase continuation: %q", cont.Command)
			return fmt.Errorf("parse rebase continuation: %w", err)
		}

		if err := kctx.Run(ctx); err != nil {
			return fmt.Errorf("continue operation %q: %w", cont.Command, err)
		}

		cont, err = store.TakeContinuation(ctx, "gs rebase continue")
		if err != nil {
			return fmt.Errorf("take rebase continuation: %w", err)
		}
	}

	return nil
}
