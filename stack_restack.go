package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/restack"
	"go.abhg.dev/gs/internal/handler/submodule"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type stackRestackCmd struct {
	Branch            string `help:"Branch to restack the stack of" placeholder:"NAME" predictor:"trackedBranches"`
	RecurseSubmodules bool   `name:"recurse-submodules" negatable:"" config:"submodule.recurse" help:"Also restack tracked submodules"`
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

	name := cli.Name()
	desc := fmt.Sprintf("Running '%[1]s %s restack' from trunk restacks all tracked branches.\n"+
		"Use '%[1]s repo restack' to suppress this prompt.", name, commandName)

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
	wt *git.Worktree,
	store *state.Store,
	cfg *spice.Config,
	handler RestackHandler,
) error {
	if err := verifyRestackFromTrunk(log, view, store, cmd.Branch, "stack"); err != nil {
		return err
	}

	if err := handler.RestackStack(ctx, cmd.Branch, nil); err != nil {
		return err
	}

	if !cmd.RecurseSubmodules {
		return nil
	}

	var exclude []string
	if cfg != nil {
		exclude = cfg.SubmoduleExclusions()
	}

	return submodule.ForEachInitializedSubmodule(
		ctx, wt, exclude, nil, log,
		func(c *submodule.Context) error {
			subCurrent, err := c.Worktree.CurrentBranch(ctx)
			if err != nil {
				log.Warn("Skipping submodule: cannot determine current branch",
					"path", c.Path, "error", err)
				return nil
			}
			log.Infof("Recursing restack into %s on %s",
				c.Path, subCurrent)
			subHandler := &restack.Handler{
				Log:      c.Log,
				Worktree: c.Worktree,
				Store:    c.Store,
				Service:  c.Service,
			}
			if _, err := subHandler.Restack(ctx, &restack.Request{
				Branch:          subCurrent,
				ContinueCommand: []string{"stack", "restack"},
				Scope:           restack.ScopeStack,
			}); err != nil {
				return fmt.Errorf(
					"submodule %s restack: %w", c.Path, err,
				)
			}
			return nil
		},
	)
}
