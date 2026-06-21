package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchSubmoduleRepointCmd struct {
	Path   string `arg:"" help:"Submodule path to repoint."`
	Branch string `short:"b" placeholder:"BRANCH" help:"Submodule branch to associate. Defaults to submodule's current branch."`
}

func (*branchSubmoduleRepointCmd) Help() string {
	return text.Dedent(`
		Changes which submodule branch is associated
		with the current parent branch.

		If --branch is not specified,
		the submodule's current branch is used.
	`)
}

func (cmd *branchSubmoduleRepointCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
) error {
	parentBranch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	branch := cmd.Branch
	if branch == "" {
		branch, err = wt.SubmoduleCurrentBranch(
			ctx, cmd.Path,
		)
		if err != nil {
			return fmt.Errorf(
				"submodule %s current branch: %w",
				cmd.Path, err,
			)
		}
	}

	tx := store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name: parentBranch,
		Submodules: map[string]string{
			cmd.Path: branch,
		},
	}); err != nil {
		return fmt.Errorf("update submodule association: %w", err)
	}

	if err := tx.Commit(ctx,
		"repoint submodule "+cmd.Path,
	); err != nil {
		return fmt.Errorf("commit state: %w", err)
	}

	log.Infof("%v: %v \u2192 %v", parentBranch, cmd.Path, branch)
	return nil
}
