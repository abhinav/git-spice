package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchCheckoutCmd struct {
	checkout.Options
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
	wt *git.Worktree,
	branchPrompt *branchPrompter,
) error {
	if cmd.Branch == "" {
		if !ui.Interactive(view) {
			return fmt.Errorf("cannot proceed without a branch name: %w", errNoPrompt)
		}

		// If a branch name is not provided,
		// list branches besides the current branch and pick one.
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			currentBranch = ""
		}

		cmd.Branch, err = branchPrompt.Prompt(ctx, &branchPromptRequest{
			Disabled: func(b git.LocalBranch) bool {
				// If detaching, allow selecting any branch,
				// including the current branch
				// or branches checked out elsewhere.
				if cmd.Detach {
					return false
				}
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

// CheckoutHandler allows checking out branches.
type CheckoutHandler interface {
	CheckoutBranch(ctx context.Context, req *checkout.Request) error
}

func (cmd *branchCheckoutCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	handler CheckoutHandler,
) error {
	return handler.CheckoutBranch(ctx, &checkout.Request{
		Branch:  cmd.Branch,
		Options: &cmd.Options,
		ShouldTrack: func(branch string) (bool, error) {
			if !ui.Interactive(view) || !cmd.TrackUntrackedPrompt {
				log.Warnf("%v: branch not tracked: run 'gs branch track'", branch)
				return false, nil
			}

			log.Warnf("%v: branch not tracked", branch)
			shouldTrack := true
			prompt := ui.NewConfirm().
				WithValue(&shouldTrack).
				WithTitle("Do you want to track this branch now?")
			if err := ui.Run(view, prompt); err != nil {
				return false, fmt.Errorf("prompt: %w", err)
			}

			return shouldTrack, nil
		},
	})
}
