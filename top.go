package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type topCmd struct {
	checkoutOptions
}

func (*topCmd) Help() string {
	return text.Dedent(`
		Checks out the top-most branch in the current branch's stack.
		If there are multiple possible top-most branches,
		a prompt will ask you to pick one.
		Use the -n flag to print the branch without checking it out.
	`)
}

func (cmd *topCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	trackHandler TrackHandler,
) error {
	current, err := wt.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	tops, err := svc.FindTop(ctx, current)
	if err != nil {
		return fmt.Errorf("find top-most branches: %w", err)
	}
	must.NotBeEmptyf(tops, "FindTopmost always returns at least one branch")

	branch := tops[0]
	if len(tops) > 1 {
		desc := "There are multiple top-level branches reachable from the current branch."
		if !ui.Interactive(view) {
			log.Error(desc)
			return errNoPrompt
		}

		items := make([]widget.BranchTreeItem, len(tops))
		for i, b := range tops {
			items[i] = widget.BranchTreeItem{
				Branch: b,
				Base:   current,
			}
		}

		// If there are multiple top-most branches,
		// prompt the user to pick one.
		prompt := widget.NewBranchTreeSelect().
			WithValue(&branch).
			WithItems(items...).
			WithTitle("Pick a branch").
			WithDescription(desc)
		if err := ui.Run(view, prompt); err != nil {
			return fmt.Errorf("a branch is required: %w", err)
		}
	}

	if branch == current && !cmd.DryRun {
		log.Info("Already on the top-most branch in this stack")
		return nil
	}

	return (&branchCheckoutCmd{
		checkoutOptions: cmd.checkoutOptions,
		Branch:          branch,
	}).Run(ctx, log, view, wt, store, svc, trackHandler)
}
