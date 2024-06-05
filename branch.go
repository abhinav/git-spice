package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

type branchCmd struct {
	Track    branchTrackCmd    `cmd:"" aliases:"tr" help:"Track a branch"`
	Untrack  branchUntrackCmd  `cmd:"" aliases:"untr" help:"Forget a tracked branch"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" help:"Switch to a branch"`
	Onto     branchOntoCmd     `cmd:"" aliases:"on" help:"Move a branch onto another branch"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"d,rm" help:"Delete a branch"`
	Fold   branchFoldCmd   `cmd:"" aliases:"fo" help:"Merge a branch into its base"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the commits in a branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"rn,mv" help:"Rename a branch"`
	Restack branchRestackCmd `cmd:"" aliases:"r" help:"Restack a branch"`

	// Pull request management
	Submit branchSubmitCmd `cmd:"" aliases:"s" help:"Submit a branch"`
}

// branchPrompt prompts a user to select a local branch
// that may or may not be tracked by the store.
type branchPrompt struct {
	// Exclude specifies branches that will not be included in the list.
	Exclude []string

	// ExcludeCheckedOut specifies whether branches that are checked out
	// in any worktree should be excluded.
	ExcludeCheckedOut bool

	// TrackedOnly indicates that only tracked branches and Trunk
	// should be included in the list.
	TrackedOnly bool

	// Title specifies the prompt to display to the user.
	Title string

	// Description specifies the description to display to the user.
	Description string
}

func (p *branchPrompt) Run(ctx context.Context, repo *git.Repository, store *state.Store) (string, error) {
	var filters []func(git.LocalBranch) bool

	if len(p.Exclude) > 0 {
		slices.Sort(p.Exclude)
		filters = append(filters, func(b git.LocalBranch) bool {
			_, ok := slices.BinarySearch(p.Exclude, b.Name)
			return !ok
		})
	}

	if p.ExcludeCheckedOut {
		filters = append(filters, func(b git.LocalBranch) bool {
			return !b.CheckedOut
		})
	}

	trunk := store.Trunk()
	if p.TrackedOnly {
		tracked, err := store.ListBranches(ctx)
		if err != nil {
			return "", fmt.Errorf("list tracked branches: %w", err)
		}
		slices.Sort(tracked)

		filters = append(filters, func(b git.LocalBranch) bool {
			if b.Name == trunk {
				// Always include Trunk
				return true
			}
			_, ok := slices.BinarySearch(tracked, b.Name)
			return ok
		})
	}

	localBranches, err := repo.LocalBranches(ctx)
	if err != nil {
		return "", fmt.Errorf("list branches: %w", err)
	}

	branches := make([]string, 0, len(localBranches))
nextBranch:
	for _, branch := range localBranches {
		for _, filter := range filters {
			if !filter(branch) {
				continue nextBranch
			}
		}

		branches = append(branches, branch.Name)
	}
	sort.Strings(branches)

	if len(branches) == 0 {
		return "", errors.New("no branches available")
	}

	prompt := ui.NewSelect().
		WithOptions(branches...).
		WithTitle(p.Title).
		WithDescription(p.Description)
	if err := ui.Run(prompt); err != nil {
		return "", fmt.Errorf("select branch: %w", err)
	}

	return prompt.Value(), nil
}
