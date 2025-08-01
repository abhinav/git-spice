package main

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type upCmd struct {
	checkout.Options

	N int `arg:"" optional:"" help:"Number of branches to move up." default:"1"`
}

func (*upCmd) Help() string {
	return text.Dedent(`
		Checks out the branch above the current one.
		If there are multiple branches with the current branch as base,
		a prompt will allow picking between them.
		Use the -n flag to print the branch without checking it out.
	`)
}

func (cmd *upCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	wt *git.Worktree,
	svc *spice.Service,
	checkoutHandler CheckoutHandler,
) error {
	current, err := wt.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("load branch graph: %w", err)
	}

	var branch string
outer:
	for range cmd.N {
		aboves := slices.Collect(graph.Aboves(current))
		switch len(aboves) {
		case 0:
			if branch != "" {
				// If we've already moved up once,
				// and there are no branches above the current one,
				// we're done.
				break outer
			}
			return fmt.Errorf("%v: no branches found upstack", current)
		case 1:
			branch = aboves[0]
		default:
			desc := "There are multiple branches above the current branch."
			if !ui.Interactive(view) {
				log.Error(desc)
				return errNoPrompt
			}

			items := make([]widget.BranchTreeItem, len(aboves))
			for i, b := range aboves {
				items[i] = widget.BranchTreeItem{
					Branch: b,
					Base:   current,
				}
			}

			prompt := widget.NewBranchTreeSelect().
				WithValue(&branch).
				WithItems(items...).
				WithTitle("Pick a branch").
				WithDescription(desc)
			if err := ui.Run(view, prompt); err != nil {
				return fmt.Errorf("a branch is required: %w", err)
			}
		}

		current = branch
	}

	return checkoutHandler.CheckoutBranch(ctx, &checkout.Request{
		Branch:  branch,
		Options: &cmd.Options,
	})
}
