package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/ui"
)

type branchCmd struct {
	Track    branchTrackCmd    `cmd:"" aliases:"tr" help:"Track a branch"`
	Untrack  branchUntrackCmd  `cmd:"" aliases:"untr" help:"Forget a tracked branch"`
	Checkout branchCheckoutCmd `cmd:"" aliases:"co" help:"Switch to a branch"`
	Onto     branchOntoCmd     `cmd:"" aliases:"on" help:"Move a branch onto another branch"`

	// Creation and destruction
	Create branchCreateCmd `cmd:"" aliases:"c" help:"Create a new branch"`
	Delete branchDeleteCmd `cmd:"" aliases:"rm" help:"Delete a branch"`
	Fold   branchFoldCmd   `cmd:"" aliases:"fo" help:"Merge a branch into its base"`

	// Mutation
	Edit    branchEditCmd    `cmd:"" aliases:"e" help:"Edit the commits in a branch"`
	Rename  branchRenameCmd  `cmd:"" aliases:"mv" help:"Rename a branch"`
	Restack branchRestackCmd `cmd:"" aliases:"r" help:"Restack a branch"`

	// Pull request management
	Submit branchSubmitCmd `cmd:"" aliases:"s" help:"Submit a branch"`
}

// branchPrompt prompts a user to select a branch.
type branchPrompt struct {
	// Exclude specifies branches that will not be included in the list.
	Exclude []string

	// ExcludeCheckedOut specifies whether branches that are checked out
	// in any worktree should be excluded.
	ExcludeCheckedOut bool

	// Title specifies the prompt to display to the user.
	Title string

	// Description specifies the description to display to the user.
	Description string
}

func (p *branchPrompt) Run(ctx context.Context, repo *git.Repository) (string, error) {
	slices.Sort(p.Exclude)

	branches, err := repo.LocalBranches(ctx)
	if err != nil {
		return "", fmt.Errorf("list branches: %w", err)
	}

	branchNames := make([]string, 0, len(branches))
	for _, branch := range branches {
		if p.ExcludeCheckedOut && branch.CheckedOut {
			continue
		}

		if _, ok := slices.BinarySearch(p.Exclude, branch.Name); ok {
			continue
		}

		branchNames = append(branchNames, branch.Name)
	}
	sort.Strings(branchNames)

	if len(branchNames) == 0 {
		return "", errors.New("no branches available")
	}

	var result string
	prompt := ui.NewSelect(&result, branchNames...).
		WithTitle(p.Title).
		WithDescription(p.Description)
	if err := ui.Run(prompt); err != nil {
		return "", fmt.Errorf("select branch: %w", err)
	}

	return result, nil
}
