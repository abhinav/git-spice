package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type stackRestackCmd struct {
	Branch string `help:"Branch to restack the stack of" placeholder:"NAME" predictor:"trackedBranches"`
}

func (*stackRestackCmd) Help() string {
	return text.Dedent(`
		All branches in the current stack are rebased on top of their
		respective bases, ensuring a linear history.

		Use --branch to rebase the stack of a different branch.
	`)
}

// verifyRestackFromTrunk checks if we're restacking from trunk
// and prompts for confirmation if so.
//
// Returns nil if the operation should proceed, or an error.
func verifyRestackFromTrunk(
	log *silog.Logger,
	view ui.View,
	store *state.Store,
	currentBranch string,
	commandName string,
) error {
	if currentBranch != store.Trunk() {
		return nil
	}

	desc := fmt.Sprintf("Running 'gs %s restack' from trunk restacks all tracked branches.\n"+
		"Use 'gs repo restack' to suppress this prompt.", commandName)

	// Non-interactive mode: print warning and proceed
	if !ui.Interactive(view) {
		log.Warn(desc)
		return nil
	}

	proceed := true // default true so user can "enter" to accept
	confirm := ui.NewConfirm().
		WithTitle("Restack all branches?").
		WithDescription(desc).
		WithValue(&proceed)
	if err := ui.Run(view, confirm); err != nil {
		return fmt.Errorf("run prompt: %w", err)
	}

	if !proceed {
		return errors.New("operation aborted")
	}
	return nil
}

func (cmd *stackRestackCmd) AfterApply(ctx context.Context, wt *git.Worktree) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}
	return nil
}

func (cmd *stackRestackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	store *state.Store,
	handler RestackHandler,
) error {
	if err := verifyRestackFromTrunk(log, view, store, cmd.Branch, "stack"); err != nil {
		return err
	}

	return handler.RestackStack(ctx, cmd.Branch)
}
