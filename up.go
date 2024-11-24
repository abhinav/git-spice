package main

import (
	"context"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type upCmd struct {
	checkoutOptions

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

func (cmd *upCmd) Run(ctx context.Context, log *log.Logger, view ui.View) error {
	repo, _, svc, err := openRepo(ctx, log, view)
	if err != nil {
		return err
	}

	current, err := repo.CurrentBranch(ctx)
	if err != nil {
		// TODO: handle not a branch
		return fmt.Errorf("get current branch: %w", err)
	}

	var branch string
outer:
	for range cmd.N {
		aboves, err := svc.ListAbove(ctx, current)
		if err != nil {
			return fmt.Errorf("list branches above %v: %w", current, err)
		}

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

	return (&branchCheckoutCmd{
		checkoutOptions: cmd.checkoutOptions,
		Branch:          branch,
	}).Run(ctx, log, view)
}
