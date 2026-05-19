package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/checkout"
	"go.abhg.dev/gs/internal/handler/submodule"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/ui"
)

type branchFoldCmd struct {
	Branch       string            `placeholder:"NAME" help:"Name of the branch" predictor:"trackedBranches"`
	ModuleBranch map[string]string `name:"module-branch" placeholder:"PATH=BRANCH" help:"Per-submodule branch override for fold conflicts (repeatable)"`
}

func (*branchFoldCmd) Help() string {
	return text.Dedent(`
		Commits from the current branch will be merged into its base
		and the current branch will be deleted.
		Branches above the folded branch will point
		to the next branch downstack.

		Use the --branch flag to target a different branch.
	`)
}

func (cmd *branchFoldCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	view ui.View,
	repo *git.Repository,
	wt *git.Worktree,
	store *state.Store,
	svc *spice.Service,
	checkoutHandler CheckoutHandler,
	submoduleApplier SubmoduleApplier,
) error {
	if cmd.Branch == "" {
		currentBranch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = currentBranch
	}

	if err := svc.VerifyRestacked(ctx, cmd.Branch); err != nil {
		var restackErr *spice.BranchNeedsRestackError
		switch {
		case errors.Is(err, state.ErrNotExist):
			return fmt.Errorf("branch %v not tracked", cmd.Branch)
		case errors.As(err, &restackErr):
			return fmt.Errorf("branch %v needs to be restacked before it can be folded", cmd.Branch)
		default:
			return fmt.Errorf("verify restacked: %w", err)
		}
	}

	b, err := svc.LookupBranch(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}

	// Check if we're about to fold onto the trunk branch
	if b.Base == store.Trunk() {
		if !ui.Interactive(view) {
			log.Warnf("You are about to fold branch %v onto the trunk branch (%v).", cmd.Branch, store.Trunk())
		} else {
			var proceed bool
			prompt := ui.NewConfirm().
				WithTitlef("Fold branch onto trunk?").
				WithDescriptionf("You are about to fold branch %v onto the trunk branch (%v). "+
					"This is usually not what you want to do.", cmd.Branch, store.Trunk()).
				WithValue(&proceed)
			if err := ui.Run(view, prompt); err != nil {
				return fmt.Errorf("run prompt: %w", err)
			}
			if !proceed {
				return errors.New("operation aborted")
			}
		}
	}

	// Merge base into current branch using a fast-forward.
	// To do this without checking out the base, we can use a local fetch
	// and fetch the feature branch "into" the base branch.
	if err := repo.Fetch(ctx, git.FetchOptions{
		Remote: ".", // local repository
		Refspecs: []git.Refspec{
			git.Refspec(cmd.Branch + ":" + b.Base),
		},
	}); err != nil {
		return fmt.Errorf("update base branch: %w", err)
	}

	newBaseHash, err := repo.PeelToCommit(ctx, b.Base)
	if err != nil {
		return fmt.Errorf("peel to commit: %w", err)
	}

	// Merge submodule associations from child into base.
	// The child branch is being folded away; its sub-branch records
	// should win when they differ from the base's, but conflicting
	// per-sub records require explicit resolution.
	type submoduleApplierWithMerge interface {
		MergeAssociationsForFold(
			ctx context.Context, req submodule.MergeFoldRequest,
		) (map[string]string, error)
	}
	merge, _ := submoduleApplier.(submoduleApplierWithMerge)
	var resolvedSubs map[string]string
	if merge != nil && b.Base != store.Trunk() {
		// Trunk is not tracked in the store; nothing to merge against.
		var resolveFn func(submodule.FoldConflict) (string, error)
		if ui.Interactive(view) {
			resolveFn = func(c submodule.FoldConflict) (string, error) {
				var pick string
				prompt := ui.NewSelect[string]().
					WithValue(&pick).
					WithOptions(
						ui.SelectOption[string]{
							Label: c.ChildBranch,
							Value: c.ChildBranch,
						},
						ui.SelectOption[string]{
							Label: c.BaseBranch,
							Value: c.BaseBranch,
						},
					).
					WithTitle(fmt.Sprintf(
						"Submodule %s: pick branch for %s",
						c.Path, b.Base)).
					WithDescription(fmt.Sprintf(
						"Folding %s (records %s) into %s (records %s)",
						cmd.Branch, c.ChildBranch,
						b.Base, c.BaseBranch))
				if err := ui.Run(view, prompt); err != nil {
					return "", err
				}
				return pick, nil
			}
		}
		var err error
		resolvedSubs, err = merge.MergeAssociationsForFold(ctx, submodule.MergeFoldRequest{
			Base:         b.Base,
			Child:        cmd.Branch,
			ModuleBranch: cmd.ModuleBranch,
			Resolve:      resolveFn,
		})
		if err != nil {
			return fmt.Errorf("merge submodule associations: %w", err)
		}
	}

	tx := store.BeginBranchTx()

	// Persist the merged submodule associations onto the base.
	if len(resolvedSubs) > 0 {
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name:       b.Base,
			Submodules: resolvedSubs,
		}); err != nil {
			return fmt.Errorf(
				"set submodule associations on %v: %w", b.Base, err,
			)
		}
	}

	// Change the base of all branches above us
	// to the base of the branch we are folding.
	aboves, err := svc.ListAbove(ctx, cmd.Branch)
	if err != nil {
		return fmt.Errorf("list above: %w", err)
	}

	for _, above := range aboves {
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name:     above,
			Base:     b.Base,
			BaseHash: newBaseHash,
		}); err != nil {
			return fmt.Errorf("set base of %v to %v: %w", above, b.Base, err)
		}
		log.Debug("Changing base branch of upstream",
			"branch", above,
			"newBase", b.Base)
	}

	if err := tx.Delete(ctx, cmd.Branch); err != nil {
		return fmt.Errorf("delete branch %v from state: %w", cmd.Branch, err)
	}
	log.Debug("Removing branch from tracking", "branch", cmd.Branch)

	if err := tx.Commit(ctx, fmt.Sprintf("folding %v into %v", cmd.Branch, b.Base)); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	// Check out base and delete the branch we are folding.
	if err := checkoutHandler.CheckoutBranch(ctx, &checkout.Request{Branch: b.Base}); err != nil {
		return fmt.Errorf("checkout base: %w", err)
	}

	if err := repo.DeleteBranch(ctx, cmd.Branch, git.BranchDeleteOptions{
		Force: true, // we know it's merged
	}); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}

	log.Infof("Branch %v has been folded into %v", cmd.Branch, b.Base)
	return nil
}
