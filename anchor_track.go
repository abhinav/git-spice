package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type anchorTrackCmd struct {
	Name string `name:"name" placeholder:"BRANCH" help:"Branch to register as this worktree's anchor (defaults to the branch checked out in the current worktree)"`
}

func (*anchorTrackCmd) Help() string {
	return text.Dedent(`
		Registers an existing worktree's anchor branch with git-spice.

		Use this to adopt a worktree created outside of
		'gs anchor create' (for example, with 'git worktree add'):
		it records the worktree's anchor branch so that
		'gs repo sync', restacks, and stack listings treat that branch
		as an anchor, rooting the worktree's stacks at it instead of the
		shared canonical trunk.

		By default, the branch checked out in the current worktree is
		registered. Use --name to register a different existing branch.

		If the branch has no upstream, it is set to track the same
		remote ref as the canonical trunk, so that 'gs repo sync' in
		the worktree updates it from the remote.
	`)
}

func (cmd *anchorTrackCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
	wt *git.Worktree,
) error {
	// Resolve the branch to register as this worktree's anchor:
	// the explicit --name, or the branch checked out here.
	anchorBranch := cmd.Name
	if anchorBranch == "" {
		var err error
		anchorBranch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("worktree is in detached HEAD; "+
				"check out an anchor branch or pass --name: %w", err)
		}
	}

	canonicalTrunk := store.Trunk()
	if anchorBranch == canonicalTrunk {
		return fmt.Errorf("branch %q is the canonical trunk; "+
			"the primary worktree does not need an anchor", anchorBranch)
	}

	if !repo.BranchExists(ctx, anchorBranch) {
		return fmt.Errorf("branch %q does not exist", anchorBranch)
	}

	// An anchor is a graph root, so it cannot also be a tracked stack
	// branch. This mirrors the inverse guard in the branch tracking
	// path, which refuses to track an anchor as a stack branch.
	if _, err := svc.LookupBranch(ctx, anchorBranch); err == nil {
		return fmt.Errorf("branch %q is tracked as a stack branch; "+
			"an anchor must be a graph root", anchorBranch)
	} else if !errors.Is(err, state.ErrNotExist) {
		return fmt.Errorf("look up branch %q: %w", anchorBranch, err)
	}

	if err := store.RegisterAnchor(ctx, state.Anchor{
		Branch:   anchorBranch,
		Worktree: wt.RootDir(),
	}); err != nil {
		return fmt.Errorf("register anchor: %w", err)
	}
	log.Infof("Tracking %s as the anchor for worktree %s", anchorBranch, wt.RootDir())

	// Point the anchor at the remote canonical trunk so 'gs repo sync'
	// can update it, but only if it has no upstream already: an existing
	// upstream is the user's choice and must not be clobbered.
	if _, err := repo.BranchUpstream(ctx, anchorBranch); errors.Is(err, git.ErrNotExist) {
		if remote, rerr := store.Remote(); rerr == nil {
			upstream := cmp.Or(remote.Upstream, remote.Push) + "/" + canonicalTrunk
			if serr := repo.SetBranchUpstream(ctx, anchorBranch, upstream); serr != nil {
				log.Warnf("Could not set upstream of %q to %q: %v", anchorBranch, upstream, serr)
			} else {
				log.Infof("Set upstream of %s to %s", anchorBranch, upstream)
			}
		}
	}

	return nil
}
