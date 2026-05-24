package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/merge"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type downstackMergeCmd struct {
	Branch        string        `placeholder:"NAME" help:"Branch to start merging from" predictor:"trackedBranches"`
	NoWait        bool          `help:"Skip polling for each merge to propagate (still retargets and cleans up)."`
	NoBranchCheck bool          `help:"Skip stale base validation before merging."`
	BuildTimeout  time.Duration `config:"merge.buildTimeout" default:"30m" help:"Max time to wait for CI checks before each merge. 0 means check once."`
}

func (*downstackMergeCmd) Help() string {
	return text.Dedent(`
		Merges the current branch and all branches below it
		into trunk via the forge API, bottom-up.
		Use --branch to start at a different branch.

		This command acts as a local merge queue:
		it merges one Change Request,
		waits for that merge to finish,
		retargets the next Change Request to trunk,
		and then repeats the process.

		For a stack like this:

		    main <- feature1 <- feature2 <- feature3

		Running from feature3 merges in this order:

		    feature1, feature2, feature3

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

func (cmd *downstackMergeCmd) AfterApply(
	ctx context.Context,
	wt *git.Worktree,
) error {
	if cmd.Branch != "" {
		return nil
	}
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	cmd.Branch = branch
	return nil
}

func (cmd *downstackMergeCmd) Run(
	ctx context.Context,
	store *state.Store,
	mergeHandler MergeHandler,
) error {
	if cmd.Branch == store.Trunk() {
		return errors.New("cannot merge trunk")
	}

	return mergeHandler.MergeDownstack(ctx, &merge.Request{
		Branch:        cmd.Branch,
		NoWait:        cmd.NoWait,
		NoBranchCheck: cmd.NoBranchCheck,
		BuildTimeout:  cmd.BuildTimeout,
	})
}
