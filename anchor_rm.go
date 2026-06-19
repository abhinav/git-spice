package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type anchorRmCmd struct {
	Path  string `arg:"" help:"Path of the anchor worktree to remove"`
	Force bool   `help:"Remove the worktree even if it has uncommitted changes"`
}

func (*anchorRmCmd) Help() string {
	return text.Dedent(`
		Removes an anchor worktree and dissolves its anchor.

		The anchor's direct child branches are retargeted onto the
		anchor's base (the canonical trunk for a root anchor, or the
		pinned branch for an internal anchor); they are left needing a
		restack. The anchor branch is then deleted and the worktree
		directory removed.

		Use this when a worktree's work is done. Run it from a different
		worktree (for example, the primary checkout): a worktree cannot
		remove itself.
	`)
}

func (cmd *anchorRmCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	// Resolve the path to a worktree root and find its anchor.
	target, err := repo.OpenWorktree(ctx, cmd.Path)
	if err != nil {
		return fmt.Errorf("open worktree %q: %w", cmd.Path, err)
	}
	rootDir := target.RootDir()

	var (
		anchor state.Anchor
		found  bool
	)
	for _, a := range store.Anchors() {
		if a.Worktree == rootDir {
			anchor, found = a, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%v is not an anchor worktree", cmd.Path)
	}

	// The base onto which the anchor's children are retargeted:
	// the pinned branch for an internal anchor, else the canonical trunk.
	base := cmp.Or(anchor.Base, store.Trunk())

	// Collect the anchor's direct children before mutating anything.
	graph, err := svc.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("load branch graph: %w", err)
	}
	children := slices.Collect(graph.Aboves(anchor.Branch))

	// Retarget each direct child onto the anchor's base. Retarget-only
	// updates state without rebasing, so the children are left needing
	// a restack rather than risking a conflict during removal.
	for _, child := range children {
		if err := svc.BranchOnto(ctx, &spice.BranchOntoRequest{
			Branch: child,
			Onto:   base,
			Mode:   spice.BranchOntoRetargetOnly,
		}); err != nil {
			return fmt.Errorf("retarget %q onto %q: %w", child, base, err)
		}
		log.Infof("%v: retargeted onto %v", child, base)
	}

	// Remove the worktree directory (refs untouched), then tear out the
	// anchor: delete its pointer branch and drop its registration.
	if err := repo.WorktreeRemove(ctx, git.WorktreeRemoveRequest{
		Path:  rootDir,
		Force: cmd.Force,
	}); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	log.Infof("Removed worktree %s", cmd.Path)

	if err := repo.DeleteBranch(ctx, anchor.Branch, git.BranchDeleteOptions{
		Force: true,
	}); err != nil {
		return fmt.Errorf("delete anchor branch %q: %w", anchor.Branch, err)
	}

	if err := store.UnregisterAnchor(ctx, anchor.Branch); err != nil {
		return fmt.Errorf("unregister anchor: %w", err)
	}
	log.Infof("Dissolved anchor %s", anchor.Branch)

	return nil
}
