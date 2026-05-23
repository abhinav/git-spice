package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/merge"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchMergeCmd struct {
	Branch        string        `placeholder:"NAME" help:"Branch to merge" predictor:"trackedBranches"`
	NoWait        bool          `help:"Skip polling for each merge to propagate (still retargets and cleans up)."`
	NoBranchCheck bool          `help:"Skip stale base validation before merging."`
	BuildTimeout  time.Duration `config:"merge.buildTimeout" default:"30m" help:"Max time to wait for CI checks before each merge. 0 means check once."`
}

func (*branchMergeCmd) Help() string {
	return text.Dedent(`
		Merges the current branch and all branches below it
		into trunk via the forge API, bottom-up.
		Use --branch to start at a different branch.

		Already-merged branches are skipped automatically.
		Branches must have an open Change Request to be merged.

		Before merging, the downstack is checked for branches
		whose base PR was already merged on the forge.
		Use --no-branch-check to skip this validation.

		Before each merge, waits for CI checks to pass.
		Use --build-timeout to configure the maximum wait
		(default: 30m, 0 means fail immediately if not ready).

		Between merges, the command waits for each merge
		to complete, retargets the next PR to trunk,
		and cleans up the merged local branch.
		Use --no-wait to skip the propagation polling.
	`)
}

// MergeHandler merges change requests via a forge.
type MergeHandler interface {
	MergeDownstack(ctx context.Context, req *merge.Request) error
}

func (cmd *branchMergeCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	forgeRepo forge.Repository,
	mergeHandler MergeHandler,
	log *silog.Logger,
) error {
	branch, err := cmd.resolveBranch(ctx, wt)
	if err != nil {
		return err
	}

	if branch == store.Trunk() {
		return errors.New("cannot merge trunk")
	}

	if err := cmd.checkDownstack(
		ctx, svc, forgeRepo, log, branch,
	); err != nil {
		return err
	}

	return mergeHandler.MergeDownstack(ctx, &merge.Request{
		Branch:       branch,
		NoWait:       cmd.NoWait,
		BuildTimeout: cmd.BuildTimeout,
	})
}

func (cmd *branchMergeCmd) resolveBranch(
	ctx context.Context, wt *git.Worktree,
) (string, error) {
	if cmd.Branch != "" {
		return cmd.Branch, nil
	}
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return branch, nil
}

func (cmd *branchMergeCmd) checkDownstack(
	ctx context.Context,
	svc *spice.Service,
	forgeRepo forge.Repository,
	log *silog.Logger,
	branch string,
) error {
	if cmd.NoBranchCheck {
		return nil
	}

	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	staleBases, err := spice.FindStaleBases(
		ctx, graph, forgeRepo, []string{branch},
	)
	if err != nil {
		return err
	}
	if len(staleBases) == 0 {
		return nil
	}

	for _, staleBase := range staleBases {
		log.Warn("Branch has stale base",
			"branch", staleBase.Branch,
			"base", staleBase.Base,
		)
	}
	return fmt.Errorf(
		"%d branches with stale bases were found; "+
			"run 'gs repo sync' first, "+
			"or use --no-branch-check to merge anyway",
		len(staleBases),
	)
}
