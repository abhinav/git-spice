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
	"go.abhg.dev/gs/internal/ui"
)

type checkoutOptions struct {
	DryRun bool `short:"n" xor:"detach-or-dry-run" help:"Print the target branch without checking it out"`
	Detach bool `xor:"detach-or-dry-run" help:"Detach HEAD after checking out"`
}

type branchCheckoutCmd struct {
	checkoutOptions
	BranchPromptConfig

	// Allow users to opt out of the "branch not tracked" prompt.
	TrackUntrackedPrompt bool `config:"branchCheckout.trackUntrackedPrompt" hidden:"" default:"true" help:"Whether to prompt to track untracked branches when checked out."`

	Untracked bool   `short:"u" config:"branchCheckout.showUntracked" help:"Show untracked branches if one isn't supplied"`
	Branch    string `arg:"" optional:"" help:"Name of the branch to checkout" predictor:"branches"`
}

func (*branchCheckoutCmd) Help() string {
	return text.Dedent(`
		A prompt will allow selecting between tracked branches.
		Provide a branch name as an argument to skip the prompt.

		Use -u/--untracked to show untracked branches in the prompt.

		Use the spice.branchPrompt.sort configuration option
		to specify the sort order of branches in the prompt.
		Commonly used field names include "refname", "commiterdate", etc.
		By default, branches are sorted by name.
	`)
}

// AfterApply runs after command line options have been parsed
// but before the command is executed.
//
// We'll use this to fill in the branch name if it's missing.
func (cmd *branchCheckoutCmd) AfterApply(
	ctx context.Context,
	view ui.View,
	repo *git.Repository,
	branchPrompt *branchPrompter,
) error {
	if cmd.Branch == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a branch name: %w", errNoPrompt)
		}

		// If a branch name is not provided,
		// list branches besides the current branch and pick one.
		currentBranch, err := repo.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		cmd.Branch, err = branchPrompt.Prompt(ctx, &branchPromptRequest{
			Disabled: func(b git.LocalBranch) bool {
				return b.Name != currentBranch && b.Worktree != ""
			},
			Default:     currentBranch,
			TrackedOnly: !cmd.Untracked,
			Title:       "Select a branch to checkout",
		})
		if err != nil {
			return fmt.Errorf("select branch: %w", err)
		}
	}

	return nil
}

func (cmd *branchCheckoutCmd) Run(
	ctx context.Context,
	log *log.Logger,
	view ui.View,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	if cmd.Branch != store.Trunk() {
		if err := svc.VerifyRestacked(ctx, cmd.Branch); err != nil {
			var restackErr *spice.BranchNeedsRestackError
			switch {
			case errors.As(err, &restackErr):
				log.Warnf("%v: needs to be restacked: run 'gs branch restack --branch=%v'", cmd.Branch, cmd.Branch)
			case errors.Is(err, state.ErrNotExist):
				if !ui.Interactive(view) || !cmd.TrackUntrackedPrompt {
					log.Warnf("%v: branch not tracked: run 'gs branch track'", cmd.Branch)
					break
				}

				log.Warnf("%v: branch not tracked", cmd.Branch)
				track := true
				prompt := ui.NewConfirm().
					WithValue(&track).
					WithTitle("Do you want to track this branch now?")
				if err := ui.Run(view, prompt); err != nil {
					return fmt.Errorf("prompt: %w", err)
				}

				if track {
					err := (&branchTrackCmd{
						Branch: cmd.Branch,
					}).Run(ctx, log, repo, store, svc)
					if err != nil {
						return fmt.Errorf("track branch: %w", err)
					}
				}
			case errors.Is(err, git.ErrNotExist):
				return fmt.Errorf("branch %q does not exist", cmd.Branch)
			default:
				log.Warnf("error checking branch: %v", err)
			}
		}
	}

	if cmd.DryRun {
		fmt.Println(cmd.Branch)
		return nil
	}

	if cmd.Detach {
		if err := repo.DetachHead(ctx, cmd.Branch); err != nil {
			return fmt.Errorf("detach HEAD: %w", err)
		}

		return nil
	}

	if err := repo.Checkout(ctx, cmd.Branch); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	return nil
}
