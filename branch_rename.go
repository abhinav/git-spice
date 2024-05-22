package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/text"
)

type branchRenameCmd struct {
	// TODO: optional old name

	Name string `arg:"" optional:"" help:"New name of the branch"`
}

func (*branchRenameCmd) Help() string {
	return text.Dedent(`
		Renames a branch tracked by git-spice,
		updating internal references to the branch.

		If you renamed a branch without using this command,
		track the new branch name with 'gs branch track',
		and untrack the old name with 'gs branch untrack'.
	`)
}

func (cmd *branchRenameCmd) Run(ctx context.Context, log *log.Logger, opts *globalOptions) (err error) {
	repo, err := git.Open(ctx, ".", git.OpenOptions{
		Log: log,
	})
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}

	// TODO: prompt for a name if not provided
	if cmd.Name == "" {
		return errors.New("branch name is required")
	}

	oldName, err := repo.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	store, err := ensureStore(ctx, repo, log, opts)
	if err != nil {
		return err
	}

	svc := spice.NewService(repo, store, log)
	if err := svc.RenameBranch(ctx, oldName, cmd.Name); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}

	return nil
}
