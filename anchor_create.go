package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type anchorCreateCmd struct {
	Path string `arg:"" help:"Path for the new worktree"`

	Name     string `name:"name" placeholder:"BRANCH" help:"Name of the anchor branch to create (defaults to the worktree directory name)"`
	Anchor   string `name:"anchor" placeholder:"BRANCH" help:"Anchor on an existing tracked branch (a dependent worktree) instead of the remote trunk"`
	NoAnchor bool   `name:"no-anchor" help:"Do not create an anchor; start in detached HEAD at trunk"`

	Branch string `short:"b" placeholder:"BRANCH" help:"Create and check out a new tracked branch stacked on the anchor"`
}

func (*anchorCreateCmd) Help() string {
	return text.Dedent(`
		Creates a new Git worktree at the given path, anchored at a branch.

		By default the worktree gets a root anchor: a per-worktree pointer
		branch (named after the worktree directory, or set with --name)
		that tracks the same remote trunk as the main checkout.
		Stacks created in the worktree are based on this anchor, so
		'gs repo sync' and restacks in different worktrees never contend
		on a single shared trunk checkout.

		Use --anchor to instead pin the worktree at an existing tracked
		branch: a dependent worktree, for building on top of another
		worktree's work.

		Use -b/--branch to also create a tracked branch stacked on the
		anchor.

		Use --no-anchor to instead start the worktree in detached HEAD
		state at the current trunk commit.
	`)
}

func (cmd *anchorCreateCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	repo *git.Repository,
	store *state.Store,
	svc *spice.Service,
) error {
	if cmd.NoAnchor && cmd.Anchor != "" {
		return errors.New("--no-anchor and --anchor are mutually exclusive")
	}

	// While the repository is in exclusive mode, it belongs to the
	// parking process; new worktrees would race with its reorganization.
	if store.InExclusiveMode() {
		return errors.New("repository is in exclusive mode; " +
			"run 'gs repo restore' first")
	}

	canonicalTrunk := store.Trunk()

	// Resolve the base the worktree is anchored at: the canonical trunk
	// for a root anchor, or an existing tracked branch for an internal
	// (dependent) anchor.
	base := canonicalTrunk
	internal := cmd.Anchor != ""
	if internal {
		base = cmd.Anchor
		if base == canonicalTrunk {
			return fmt.Errorf("--anchor %q is the canonical trunk; "+
				"omit --anchor for a root anchor", base)
		}
		if !repo.BranchExists(ctx, base) {
			return fmt.Errorf("--anchor %q is not a tracked branch", base)
		}
		if _, err := svc.LookupBranch(ctx, base); err != nil {
			if errors.Is(err, state.ErrNotExist) {
				return fmt.Errorf("--anchor %q is not a tracked branch", base)
			}
			return fmt.Errorf("look up %q: %w", base, err)
		}
	}

	baseHash, err := repo.PeelToCommit(ctx, base)
	if err != nil {
		return fmt.Errorf("resolve %v: %w", base, err)
	}

	// Create the worktree in detached HEAD state at the base;
	// any branch checkout happens below inside the new worktree.
	if err := repo.WorktreeAdd(ctx, git.WorktreeAddRequest{
		Path:   cmd.Path,
		Detach: true,
		Head:   baseHash.String(),
	}); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}
	log.Infof("Created worktree at %s", cmd.Path)

	newWT, err := repo.OpenWorktree(ctx, cmd.Path)
	if err != nil {
		return fmt.Errorf("open worktree: %w", err)
	}

	// --no-anchor preserves the legacy detached-HEAD behavior.
	if cmd.NoAnchor {
		if cmd.Branch != "" {
			if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
				Name: cmd.Branch,
				Head: baseHash.String(),
			}); err != nil {
				return fmt.Errorf("create branch: %w", err)
			}
			if err := newWT.CheckoutBranch(ctx, cmd.Branch); err != nil {
				return fmt.Errorf("checkout branch: %w", err)
			}
			log.Infof("Created and checked out branch %s", cmd.Branch)
		}
		return nil
	}

	// The anchor is a per-worktree pointer branch at the base commit.
	anchorBranch := cmp.Or(cmd.Name, filepath.Base(cmd.Path))
	if anchorBranch == canonicalTrunk {
		return fmt.Errorf("anchor %q is the same as the canonical trunk; "+
			"choose another name with --name", anchorBranch)
	}

	if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
		Name: anchorBranch,
		Head: baseHash.String(),
	}); err != nil {
		return fmt.Errorf("create anchor %q: %w", anchorBranch, err)
	}
	if err := newWT.CheckoutBranch(ctx, anchorBranch); err != nil {
		return fmt.Errorf("checkout anchor: %w", err)
	}

	// A root anchor tracks the remote canonical trunk so 'gs repo sync'
	// updates it from the remote. An internal anchor is fast-forwarded
	// from its local base by sync instead (recorded in Base below), so
	// it gets no remote upstream.
	anchorBase := ""
	if internal {
		anchorBase = base
	} else if remote, err := store.Remote(); err == nil {
		upstream := cmp.Or(remote.Upstream, remote.Push) + "/" + canonicalTrunk
		if err := repo.SetBranchUpstream(ctx, anchorBranch, upstream); err != nil {
			log.Warnf("Could not set upstream of %q to %q: %v", anchorBranch, upstream, err)
		}
	}

	if err := store.RegisterAnchor(ctx, state.Anchor{
		Branch:   anchorBranch,
		Worktree: newWT.RootDir(),
		Base:     anchorBase,
	}); err != nil {
		return fmt.Errorf("register anchor: %w", err)
	}
	if internal {
		log.Infof("Created anchor %s anchored at %s", anchorBranch, base)
	} else {
		log.Infof("Created anchor %s tracking %s", anchorBranch, canonicalTrunk)
	}

	if cmd.Branch != "" {
		if err := repo.CreateBranch(ctx, git.CreateBranchRequest{
			Name: cmd.Branch,
			Head: baseHash.String(),
		}); err != nil {
			return fmt.Errorf("create branch: %w", err)
		}
		if err := newWT.CheckoutBranch(ctx, cmd.Branch); err != nil {
			return fmt.Errorf("checkout branch: %w", err)
		}

		// Track the feature branch on the anchor.
		tx := store.BeginBranchTx()
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name:     cmd.Branch,
			Base:     anchorBranch,
			BaseHash: baseHash,
		}); err != nil {
			return fmt.Errorf("track branch %q: %w", cmd.Branch, err)
		}
		if err := tx.Commit(ctx,
			fmt.Sprintf("create branch %q in worktree", cmd.Branch)); err != nil {
			return fmt.Errorf("commit branch tracking: %w", err)
		}
		log.Infof("Created and checked out branch %s", cmd.Branch)
	}

	return nil
}
