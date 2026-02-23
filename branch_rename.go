package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/cli"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchRenameCmd struct {
	OldName string `arg:"" predictor:"branches" optional:"" help:"Old name of the branch"`
	NewName string `arg:"" optional:"" help:"New name of the branch"`
}

func (*branchRenameCmd) Help() string {
	name := cli.Name()
	return text.Dedent(fmt.Sprintf(`
		The following usage modes are supported:

			# Rename <old> to <new>
			%[1]s branch rename <old> <new>

			# Rename current branch to <new>
			%[1]s branch rename <new>

			# Rename current branch interactively
			%[1]s branch rename

		If a branch was renamed outside of '%[1]s',
		for example with 'git branch -m',
		the branch tracking information will be out of date.
		To fix this,
		untrack the old branch name with '%[1]s branch untrack <old>',
		and track the new branch name with '%[1]s branch track <new>'.
	`, name))
}

func (cmd *branchRenameCmd) Run(
	ctx context.Context,
	view ui.View,
	wt *git.Worktree,
	svc *spice.Service,
) (err error) {
	oldName, newName := cmd.OldName, cmd.NewName
	// For "git-spice branch rename <new>",
	// we'll actually get oldName = <new> and newName = "".
	if oldName != "" && newName == "" {
		oldName, newName = "", oldName
	}

	if oldName == "" {
		oldName, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	if newName == "" {
		prompt := ui.NewInput().
			WithValue(&newName).
			WithTitle("New branch name").
			WithDescription(fmt.Sprintf("Renaming branch: %v", oldName)).
			WithValidate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("branch name cannot be empty")
				}
				return nil
			})

		if err := ui.Run(view, prompt); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}
	}

	must.NotBeBlankf(oldName, "old branch name must be set")
	must.NotBeBlankf(newName, "new branch name must be set")

	if err := svc.RenameBranch(ctx, oldName, newName); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}

	return nil
}
