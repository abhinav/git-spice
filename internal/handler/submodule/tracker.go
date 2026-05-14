// Package submodule provides submodule-aware operations
// for git-spice, including discovery, branch tracking,
// and recursive orchestration.
package submodule

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
)

// Tracker discovers submodules and resolves
// branch associations for the current parent branch.
type Tracker struct {
	Log      *silog.Logger
	Worktree GitWorktree
	Store    *state.Store // used by RecordBranchState
	Exclude  []string     // submodule paths to skip
}

// RecordBranchState resolves submodule associations
// and records them in the store for the given branch.
func (t *Tracker) RecordBranchState(
	ctx context.Context, branch string,
) error {
	assocs, err := t.ResolveAssociations(ctx)
	if err != nil {
		return fmt.Errorf(
			"resolve submodule associations: %w", err,
		)
	}
	return RecordAssociations(ctx, t.Store, branch, assocs)
}

// RecordWithInheritance records submodule associations for branch
// using parentBranch's recorded associations as a baseline, then
// overlaying the current state of each submodule.
//
// The resulting map gives current state precedence: if a submodule
// is on a different branch than what parentBranch records, the new
// branch records the current state. If a submodule has no association
// in the current worktree (e.g., detached HEAD), the parent's value
// is preserved.
//
// This makes `gs bc` create a child branch that is consistent with
// its parent's submodule pinning by default without an extra step.
func (t *Tracker) RecordWithInheritance(
	ctx context.Context, branch, parentBranch string,
) error {
	inherited := make(map[string]string)
	if parentBranch != "" {
		resp, err := t.Store.LookupBranch(ctx, parentBranch)
		switch {
		case err == nil:
			maps.Copy(inherited, resp.Submodules)
		case errors.Is(err, state.ErrNotExist):
			// Parent not tracked (e.g., trunk): no baseline to inherit.
		default:
			return fmt.Errorf(
				"lookup parent branch %s: %w", parentBranch, err,
			)
		}
	}

	current, err := t.ResolveAssociations(ctx)
	if err != nil {
		return fmt.Errorf(
			"resolve submodule associations: %w", err,
		)
	}
	for _, a := range current {
		inherited[a.Path] = a.Branch
	}

	if len(inherited) == 0 {
		return nil
	}

	tx := t.Store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:       branch,
		Submodules: inherited,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx,
		"record submodule associations (with inheritance)",
	)
}

// GitWorktree is the subset of [git.Worktree]
// needed by the tracker.
type GitWorktree interface {
	Submodules(ctx context.Context) ([]git.Submodule, error)
	SubmoduleCurrentBranch(ctx context.Context, path string) (string, error)
}

var _ GitWorktree = (*git.Worktree)(nil)

// BranchAssociation maps a submodule path
// to the branch it is currently on.
type BranchAssociation struct {
	Path   string
	Branch string
}

// ResolveAssociations returns the current branch association
// for each gs-initialized, non-excluded submodule.
// Submodules in detached HEAD state are skipped.
func (t *Tracker) ResolveAssociations(
	ctx context.Context,
) ([]BranchAssociation, error) {
	subs, err := t.Worktree.Submodules(ctx)
	if err != nil {
		return nil, err
	}

	var assocs []BranchAssociation
	for _, sub := range subs {
		if t.isExcluded(sub.Path) {
			t.Log.Debug("Skipping excluded submodule",
				"path", sub.Path)
			continue
		}

		assoc, err := t.resolveOne(ctx, sub.Path)
		if err != nil {
			return nil, err
		}
		if assoc != nil {
			assocs = append(assocs, *assoc)
		}
	}
	return assocs, nil
}

func (t *Tracker) resolveOne(
	ctx context.Context, path string,
) (*BranchAssociation, error) {
	branch, err := t.Worktree.SubmoduleCurrentBranch(
		ctx, path,
	)
	if err != nil {
		if errors.Is(err, git.ErrDetachedHead) {
			t.Log.Debug("Submodule in detached HEAD",
				"path", path)
			return nil, nil
		}
		return nil, err
	}
	return &BranchAssociation{
		Path:   path,
		Branch: branch,
	}, nil
}

func (t *Tracker) isExcluded(path string) bool {
	return slices.Contains(t.Exclude, path)
}

// RecordAssociations updates a branch's submodule
// associations in the store based on current submodule state.
func RecordAssociations(
	ctx context.Context,
	store *state.Store,
	branch string,
	assocs []BranchAssociation,
) error {
	if len(assocs) == 0 {
		return nil
	}

	subs := make(map[string]string, len(assocs))
	for _, a := range assocs {
		subs[a.Path] = a.Branch
	}

	tx := store.BeginBranchTx()
	if err := tx.Upsert(ctx, state.UpsertRequest{
		Name:       branch,
		Submodules: subs,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx, "record submodule associations")
}
