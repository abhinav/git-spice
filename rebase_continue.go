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
		Continues an ongoing git-spice operation interrupted by
		a git rebase after all conflicts have been resolved.
		For example, if 'gs upstack restack' gets interrupted
		because a conflict arises during the rebase,
		you can resolve the conflict and run 'gs rebase continue'
		(or its shorthand 'gs rbc') to continue the operation.

		The command can be used in place of 'git rebase --continue'
		even if a git-spice operation is not currently in progress.
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

	if _, err := repo.RebaseState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoRebase) {
			return fmt.Errorf("get rebase state: %w", err)
		}
		return errors.New("no rebase in progress")
	}

	// Finish the ongoing rebase.
	if err := repo.RebaseContinue(ctx); err != nil {
		return svc.RebaseRescue(ctx, spice.RebaseRescueRequest{
			Err: err,
		})
	}

	cont, err := store.TakeContinuation(ctx, "gs rebase continue")
	if err != nil {
		return fmt.Errorf("take rebase continuation: %w", err)
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
