package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchRenameCmd struct {
	OldName string `arg:"" optional:"" help:"Old name of the branch"`
	NewName string `arg:"" optional:"" help:"New name of the branch"`
}

func (*branchRenameCmd) Help() string {
	return text.Dedent(`
		The following modes are supported:

			# Rename <old> to <new>
			gs branch rename <old> <new>

			# Rename current branch to <new>
			gs branch rename <new>

			# Rename current branch interactively
			gs branch rename

		For branches renamed with 'git branch -m',
		use 'gs branch track' and 'gs branch untrack'
		to update the branch tracking.
	`)
}

func (cmd *branchRenameCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	repo, _, svc, err := openRepo(ctx, log, opts)
	if err != nil {
		return err
	}

	oldName, newName := cmd.OldName, cmd.NewName
	// For "gs branch rename <new>",
	// we'll actually get oldName = <new> and newName = "".
	if oldName != "" && newName == "" {
		oldName, newName = "", oldName
	}

	if oldName == "" {
		oldName, err = repo.CurrentBranch(ctx)
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
					return fmt.Errorf("branch name cannot be empty")
				}
				return nil
			})

		if err := ui.Run(prompt); err != nil {
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
