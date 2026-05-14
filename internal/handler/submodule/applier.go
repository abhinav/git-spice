package submodule

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
)

// ApplierGitWorktree is the subset of [*git.Worktree]
// required by [Applier]. Tests mock it.
type ApplierGitWorktree interface {
	Submodules(ctx context.Context) ([]git.Submodule, error)
	SubmoduleStatus(ctx context.Context, path string) (*git.SubmoduleStatus, error)
	SubmoduleWorktree(ctx context.Context, path string) (*git.Worktree, error)
}

var _ ApplierGitWorktree = (*git.Worktree)(nil)

// ApplierStore is the subset of [*state.Store]
// required by [Applier]. Tests mock it.
type ApplierStore interface {
	LookupBranch(ctx context.Context, name string) (*state.LookupResponse, error)
}

var _ ApplierStore = (*state.Store)(nil)

// Applier performs submodule-aware operations that act on
// recorded branch associations for a parent branch.
//
// The Applier is the central place where:
//   - parent-branch -> sub-branch records are applied transactionally
//     to the working tree (used by `gs bco` and friends);
//   - sub commits and gitlink updates are coordinated at parent
//     commit time (used by `gs cc`/`gs ca`/`gs bc -m`/`gs commit fixup`);
//   - fold-time association merges are resolved.
type Applier struct {
	Log      *silog.Logger
	Worktree ApplierGitWorktree
	Store    ApplierStore
	Exclude  []string
}

// ApplyAssociations switches each tracked submodule to the branch
// recorded for parentBranch in the store, transactionally.
//
// "Tracked" means: the sub appears in the parent branch's recorded
// `Submodules` map, the sub is not in the Applier's `Exclude` list,
// and the sub is reachable from the parent worktree.
//
// The operation snapshots each sub's HEAD before switching it,
// then attempts each `git checkout <recorded>` in order. On the first
// failure, all previously-switched subs are restored to their
// snapshots and the original error is returned, wrapped with the path
// of the failing sub. The caller (typically [checkout.Handler])
// owns the parent worktree's snapshot and rollback.
//
// Subs already on the recorded branch are no-ops.
func (a *Applier) ApplyAssociations(
	ctx context.Context, parentBranch string,
) error {
	resp, err := a.Store.LookupBranch(ctx, parentBranch)
	if err != nil {
		// Branch not tracked / not in store: nothing to do.
		// Soft skip — apply only applies what is recorded.
		if errors.Is(err, state.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("lookup branch %s: %w", parentBranch, err)
	}
	if len(resp.Submodules) == 0 {
		return nil
	}

	// Iterate sub paths in deterministic order so failures
	// are reproducible and rollback order is well-defined.
	paths := make([]string, 0, len(resp.Submodules))
	for path := range resp.Submodules {
		if a.isExcluded(path) {
			a.Log.Debug("Skipping excluded submodule",
				"path", path)
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)

	var rollbacks []switchedSub

	for _, path := range paths {
		recorded := resp.Submodules[path]
		subWt, err := a.Worktree.SubmoduleWorktree(ctx, path)
		if err != nil {
			a.rollback(ctx, rollbacks)
			return fmt.Errorf(
				"submodule %s: open worktree: %w", path, err,
			)
		}

		snap, err := subWt.SnapshotHead(ctx)
		if err != nil {
			a.rollback(ctx, rollbacks)
			return fmt.Errorf(
				"submodule %s: snapshot head: %w", path, err,
			)
		}

		// Already on the recorded branch: no work.
		if !snap.Detached && snap.Branch == recorded {
			continue
		}

		if err := subWt.CheckoutBranch(ctx, recorded); err != nil {
			a.rollback(ctx, rollbacks)
			return fmt.Errorf(
				"submodule %s: checkout %s: %w",
				path, recorded, err,
			)
		}

		rollbacks = append(rollbacks, switchedSub{
			path: path,
			snap: snap,
		})
	}

	return nil
}

// switchedSub tracks a submodule that has been switched
// to a recorded branch and may need to be rolled back.
type switchedSub struct {
	path string
	snap *git.HeadSnapshot
}

func (a *Applier) rollback(
	ctx context.Context,
	switched []switchedSub,
) {
	// Restore in reverse order of switching.
	for _, s := range slices.Backward(switched) {
		subWt, err := a.Worktree.SubmoduleWorktree(ctx, s.path)
		if err != nil {
			a.Log.Warn("Submodule rollback failed: open worktree",
				"path", s.path, "error", err)
			continue
		}
		if err := subWt.RestoreHead(ctx, s.snap); err != nil {
			a.Log.Warn("Submodule rollback failed: restore HEAD",
				"path", s.path,
				"target", s.snap.Hash,
				"error", err)
		}
	}
}

func (a *Applier) isExcluded(path string) bool {
	return slices.Contains(a.Exclude, path)
}

// MergeFoldRequest specifies inputs for [Applier.MergeAssociationsForFold].
type MergeFoldRequest struct {
	// Base is the branch that survives the fold (the destination).
	Base string

	// Child is the branch being folded away.
	Child string

	// ModuleBranch is an optional map of submodule path -> branch name
	// that pre-resolves specific conflicts without prompting.
	// Typically set from a CLI flag like --module-branch=path=branch.
	ModuleBranch map[string]string

	// Resolve, if non-nil, is called for each unresolved conflict.
	// It receives the conflict description and must return the chosen
	// branch name.
	//
	// If Resolve is nil and ModuleBranch does not cover a conflict,
	// the merge returns a [FoldConflictError] listing all unresolved
	// conflicts.
	Resolve func(FoldConflict) (string, error)
}

// MergeAssociationsForFold computes the submodule association map that
// should be recorded on req.Base after folding req.Child into it.
//
// Per-path resolution:
//   - both branches record nothing → omit the path.
//   - only base records → keep base's value.
//   - only child records → adopt child's value (tip-state wins).
//   - both record the same value → keep it.
//   - both record different values → consult req.ModuleBranch[path];
//     if absent, call req.Resolve; if that is nil, accumulate into a
//     [FoldConflictError] and return it after scanning all conflicts.
//
// The returned map is suitable for [state.UpsertRequest.Submodules]:
// keys absent from the map are left unchanged; empty-string values
// would delete recorded entries (not produced by this method).
func (a *Applier) MergeAssociationsForFold(
	ctx context.Context, req MergeFoldRequest,
) (map[string]string, error) {
	baseSubs := map[string]string{}
	if resp, err := a.Store.LookupBranch(ctx, req.Base); err == nil {
		baseSubs = resp.Submodules
	} else if !errors.Is(err, state.ErrNotExist) {
		return nil, fmt.Errorf(
			"lookup base %s: %w", req.Base, err,
		)
	}

	childSubs := map[string]string{}
	if resp, err := a.Store.LookupBranch(ctx, req.Child); err == nil {
		childSubs = resp.Submodules
	} else if !errors.Is(err, state.ErrNotExist) {
		return nil, fmt.Errorf(
			"lookup child %s: %w", req.Child, err,
		)
	}

	pathSet := make(map[string]struct{}, len(baseSubs)+len(childSubs))
	for p := range baseSubs {
		pathSet[p] = struct{}{}
	}
	for p := range childSubs {
		pathSet[p] = struct{}{}
	}
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	resolved := make(map[string]string, len(paths))
	var conflicts []FoldConflict

	for _, p := range paths {
		bv, hasB := baseSubs[p]
		cv, hasC := childSubs[p]

		switch {
		case !hasB && !hasC:
			// nothing recorded; skip.
		case hasB && !hasC:
			resolved[p] = bv
		case !hasB && hasC:
			resolved[p] = cv
		case bv == cv:
			resolved[p] = bv
		default:
			// True conflict: both record, values differ.
			if v, ok := req.ModuleBranch[p]; ok {
				resolved[p] = v
				continue
			}
			if req.Resolve != nil {
				v, err := req.Resolve(FoldConflict{
					Path:        p,
					BaseBranch:  bv,
					ChildBranch: cv,
				})
				if err != nil {
					return nil, err
				}
				resolved[p] = v
				continue
			}
			conflicts = append(conflicts, FoldConflict{
				Path:        p,
				BaseBranch:  bv,
				ChildBranch: cv,
			})
		}
	}

	if len(conflicts) > 0 {
		return nil, &FoldConflictError{Conflicts: conflicts}
	}
	return resolved, nil
}
