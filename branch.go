package main

import (
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/ui/widget"
)

type branchCmd struct {
	Track    branchTrackCmd    `cmd:"" aliases:"tr" help:"Track a branch"`
	Untrack  branchUntrackCmd  `cmd:"" aliases:"untr" help:"Forget a tracked branch"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" help:"Switch to a branch"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"d,rm" help:"Delete a branch"`
	Fold   branchFoldCmd   `cmd:"" aliases:"fo" help:"Merge a branch into its base"`
	Split  branchSplitCmd  `cmd:"" aliases:"sp" help:"Split a branch on commits"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the commits in a branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"rn,mv" help:"Rename a branch"`
	Restack branchRestackCmd `cmd:"" aliases:"r" help:"Restack a branch"`
	Onto    branchOntoCmd    `cmd:"" aliases:"on" help:"Move a branch onto another branch"`

	// Pull request management
	Submit branchSubmitCmd `cmd:"" aliases:"s" help:"Submit a branch"`
}

// branchPrompt prompts a user to select a local branch
// that may or may not be tracked by the store.
type branchPrompt struct {
	// Disabled specifies whether the given branch is selectable.
	Disabled func(git.LocalBranch) bool

	// TrackedOnly indicates that only tracked branches and Trunk
	// should be included in the list.
	TrackedOnly bool

	// Default specifies the branch to select by default.
	Default string

	// Title specifies the prompt to display to the user.
	Title string

	// Description specifies the description to display to the user.
	Description string
}

func (p *branchPrompt) Run(ctx context.Context, repo *git.Repository, store *state.Store) (string, error) {
	disabled := func(git.LocalBranch) bool { return false }
	if p.Disabled != nil {
		disabled = p.Disabled
		// TODO: allow disabled branches to specify a reason.
		// Can be used to say "(checked out elsewhere)" or similar.
	}

	filter := func(git.LocalBranch) bool { return true }
	if p.TrackedOnly {
		trunk := store.Trunk()
		tracked, err := store.ListBranches(ctx)
		if err != nil {
			return "", fmt.Errorf("list tracked branches: %w", err)
		}
		slices.Sort(tracked)

		oldFilter := filter
		filter = func(b git.LocalBranch) bool {
			if b.Name == trunk {
				// Always consider Trunk tracked.
				return oldFilter(b)
			}

			_, ok := slices.BinarySearch(tracked, b.Name)
			return ok && oldFilter(b)
		}
	}

	localBranches, err := repo.LocalBranches(ctx)
	if err != nil {
		return "", fmt.Errorf("list branches: %w", err)
	}

	bases := make(map[string]string) // branch -> base
	for _, branch := range localBranches {
		res, err := store.LookupBranch(ctx, branch.Name)
		if err == nil {
			bases[branch.Name] = res.Base
		}
	}

	items := make([]widget.BranchTreeItem, 0, len(localBranches))
	for _, branch := range localBranches {
		if !filter(branch) {
			continue
		}
		items = append(items, widget.BranchTreeItem{
			Base:     bases[branch.Name],
			Branch:   branch.Name,
			Disabled: disabled(branch),
		})
	}

	value := p.Default
	prompt := widget.NewBranchTreeSelect().
		WithTitle(p.Title).
		WithValue(&value).
		WithItems(items...).
		WithDescription(p.Description)
	if err := ui.Run(prompt); err != nil {
		return "", fmt.Errorf("select branch: %w", err)
	}

	return value, nil
}
