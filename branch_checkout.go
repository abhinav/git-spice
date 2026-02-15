package main

import (
	"context"
	"encoding"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

// trackUntracked specifies the behavior
// when checking out an untracked branch.
type trackUntracked int

const (
	// trackUntrackedPrompt prompts the user to track the branch.
	// This is the default behavior.
	trackUntrackedPrompt trackUntracked = iota

	// trackUntrackedNever silently skips tracking.
	trackUntrackedNever

	// trackUntrackedAlways automatically tracks the branch.
	trackUntrackedAlways
)

var _ encoding.TextUnmarshaler = (*trackUntracked)(nil)

// String returns the string representation
// of the trackUntracked value.
func (t trackUntracked) String() string {
	switch t {
	case trackUntrackedPrompt:
		return "prompt"
	case trackUntrackedNever:
		return "never"
	case trackUntrackedAlways:
		return "always"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes trackUntracked from text.
// It accepts "prompt", "never"/"false", and "always"/"true".
// Matching is case-insensitive.
func (t *trackUntracked) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "prompt":
		*t = trackUntrackedPrompt
	case "never", "false":
		*t = trackUntrackedNever
	case "always", "true":
		*t = trackUntrackedAlways
	default:
		return fmt.Errorf(
			"invalid value %q:"+
				" expected prompt, never, or always",
			bs,
		)
	}
	return nil
}

type branchCheckoutCmd struct {
	checkout.Options
	BranchPromptConfig

	TrackUntracked          trackUntracked `config:"branchCheckout.trackUntracked" hidden:"" default:"prompt" help:"Whether to track untracked branches on checkout. One of 'prompt', 'never', or 'always'."`
	TrackUntrackedPromptOld *bool          `config:"branchCheckout.trackUntrackedPrompt" hidden:"" deprecated:""`

	Untracked bool   `short:"u" config:"branchCheckout.showUntracked" help:"Show untracked branches if one isn't supplied"`
	Branch    string `arg:"" optional:"" help:"Name of the branch to checkout" predictor:"branches"`
}

func (*branchCheckoutCmd) Help() string {
	return text.Dedent(`
		A prompt will allow selecting between tracked branches.
		Provide a branch name as an argument to skip the prompt.

		Use -u/--untracked to show untracked branches in the prompt.
		Use --detach to detach HEAD to the commit of the selected branch.
		Use -n to print the selected branch name to stdout
		without checking it out.
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
			Worktree:    wt.RootDir(),
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
	mode := cmd.TrackUntracked

	// Translate the deprecated option if set.
	if cmd.TrackUntrackedPromptOld != nil {
		if *cmd.TrackUntrackedPromptOld {
			mode = trackUntrackedPrompt
		} else {
			mode = trackUntrackedNever
		}

		log.Warnf("spice.branchCheckout.trackUntrackedPrompt is deprecated and will be removed in the future")
		log.Warnf("Please use spice.branchCheckout.trackUntracked=%v instead", mode.String())
	}

	return handler.CheckoutBranch(ctx, &checkout.Request{
		Branch:  cmd.Branch,
		Options: &cmd.Options,
		ShouldTrack: func(branch string) (bool, error) {
			switch mode {
			case trackUntrackedAlways:
				log.Infof("%v: automatically tracking branch", branch)
				return true, nil

			case trackUntrackedNever:
				log.Warnf("%v: branch not tracked: run 'gs branch track'", branch)
				return false, nil

			default: // trackUntrackedPrompt
				if !ui.Interactive(view) {
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
			}
		},
	})
}
