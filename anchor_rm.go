package main

import (
	"cmp"
	"context"
	"errors"
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
	wt *git.Worktree,
) error {
	// Resolve the path to a worktree root and find its anchor.
	target, err := repo.OpenWorktree(ctx, cmd.Path)
	if err != nil {
		return fmt.Errorf("open worktree %q: %w", cmd.Path, err)
	}
	rootDir := target.RootDir()

	anchor, found := findAnchorForWorktree(ctx, store, target, rootDir)
	if !found {
		return fmt.Errorf("%v is not an anchor worktree", cmd.Path)
	}

	// A worktree cannot remove itself: Git refuses to remove the current
	// working tree, so refuse early, before mutating any state.
	if wt.RootDir() == rootDir {
		return errors.New("a worktree cannot remove itself; " +
			"run 'gs anchor rm' from a different worktree")
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

	// Remove the worktree first. This is the only step that can fail for
	// a reason outside our control (a dirty tree without --force), so do
	// it before any state mutation: if it fails, nothing has changed and
	// the command is safe to retry. Refs are left untouched.
	if err := repo.WorktreeRemove(ctx, git.WorktreeRemoveRequest{
		Path:  rootDir,
		Force: cmd.Force,
	}); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	log.Infof("Removed worktree %s", cmd.Path)

	// Retarget each direct child onto the anchor's base. Retarget-only
	// updates state without rebasing, so the children are left needing
	// a restack rather than risking a conflict.
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

	// Tear out the anchor: delete its pointer branch and drop its
	// registration.
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

// findAnchorForWorktree resolves the anchor owned by a worktree.
//
// It matches the registry's recorded worktree path first, then falls
// back to the anchor branch checked out in the worktree: the recorded
// path is advisory and can be stale if the worktree moved.
func findAnchorForWorktree(
	ctx context.Context,
	store *state.Store,
	target *git.Worktree,
	rootDir string,
) (state.Anchor, bool) {
	anchors := store.Anchors()
	for _, a := range anchors {
		if a.Worktree == rootDir {
			return a, true
		}
	}

	if branch, err := target.CurrentBranch(ctx); err == nil {
		for _, a := range anchors {
			if a.Branch == branch {
				return a, true
			}
		}
	}

	return state.Anchor{}, false
}
