package main

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchSubmoduleListCmd struct {
	Branch string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to list associations for. Defaults to current branch."`
}

func (*branchSubmoduleListCmd) Help() string {
	return text.Dedent(`
		Shows the submodule branch associations
		recorded for the given branch.
		If no branch is specified,
		the current branch is used.
	`)
}

func (cmd *branchSubmoduleListCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
) error {
	branch := cmd.Branch
	if branch == "" {
		var err error
		branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	resp, err := store.LookupBranch(ctx, branch)
	if err != nil {
		return fmt.Errorf("lookup branch %v: %w", branch, err)
	}

	if len(resp.Submodules) == 0 {
		log.Infof("%v: no submodule associations", branch)
		return nil
	}

	for _, path := range slices.Sorted(maps.Keys(resp.Submodules)) {
		log.Infof("%-30s \u2192  %s", path, resp.Submodules[path])
	}
	return nil
}
