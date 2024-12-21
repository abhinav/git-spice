package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type rebaseContinueCmd struct {
	Edit bool `default:"true" negatable:"" config:"rebaseContinue.edit" help:"Whehter to open an editor to edit the commit message."`
}

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

		Use the --no-edit flag to continue without opening an editor.
		Make --no-edit the default by setting 'spice.rebaseContinue.edit' to false
		and use --edit to override it.
	`)
}

func (cmd *rebaseContinueCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	parser *kong.Kong,
) error {
	if _, err := repo.RebaseState(ctx); err != nil {
		if !errors.Is(err, git.ErrNoRebase) {
			return fmt.Errorf("get rebase state: %w", err)
		}
		return errors.New("no rebase in progress")
	}

	if !cmd.Edit {
		repo = repo.WithEditor("true")
	}

	// Finish the ongoing rebase.
	if err := repo.RebaseContinue(ctx); err != nil {
		var rebaseErr *git.RebaseInterruptError
		if errors.As(err, &rebaseErr) {
			var msg strings.Builder
			fmt.Fprintf(&msg, "There are more conflicts to resolve.\n")
			fmt.Fprintf(&msg, "Resolve them and run the following command again:\n")
			fmt.Fprintf(&msg, "  gs rebase continue\n")
			fmt.Fprintf(&msg, "To abort the remaining operations run:\n")
			fmt.Fprintf(&msg, "  git rebase --abort\n")
			log.Error(msg.String())
		}
		return err
	}

	// Once we get here, we have a clean state to continue running
	// rebase continuations on.
	// However, if any of the continuations encounters another conflict,
	// they will clear the continuation list.
	// So we'll want to grab the whole list here,
	// and push the remainder of it back on if a command fails.
	conts, err := store.TakeContinuations(ctx, "gs rebase continue")
	if err != nil {
		return fmt.Errorf("take rebase continuations: %w", err)
	}

	for idx, cont := range conts {
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
			// If the command failed, it has already printed the
			// rebase message, and appended its continuations.
			// We'll append the remainder.
			if err := store.AppendContinuations(ctx, "rebase continue", conts[idx+1:]...); err != nil {
				return fmt.Errorf("append rebase continuations: %w", err)
			}
			return err

		}
	}

	return nil
}
