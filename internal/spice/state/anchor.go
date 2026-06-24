package state

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"go.abhg.dev/gs/internal/spice/state/storage"
)

// _anchorsJSON is the store key holding the anchor registry.
// It is an additive, optional key: repositories that predate the feature
// (and older binaries) simply have no entry, in which case the canonical
// trunk is the only trunk.
const _anchorsJSON = "anchors"

// Anchor is a per-worktree trunk: a pointer branch that acts as a graph
// root for a linked worktree, so that sync and restack in different
// worktrees never contend on a single shared checkout.
//
// Every anchor is a trunk (a graph root), but the canonical trunk
// ([Store.Trunk]) is not an anchor. A root anchor (empty [Anchor.Base])
// tracks the same remote ref as the canonical trunk; an internal anchor
// is pinned at another local branch named by [Anchor.Base], so a worktree
// can depend on another worktree's work.
type Anchor struct {
	// Branch is the anchor branch: this worktree's graph root.
	Branch string

	// Worktree is the last-known absolute root of the owning worktree.
	// It is advisory only: worktrees can move, so the authoritative
	// signal is registry membership plus the branch's own git upstream.
	Worktree string

	// Base is the local branch this anchor is pinned at, for an
	// internal anchor. Empty for a root anchor, which tracks the
	// remote canonical trunk instead.
	Base string
}

// anchorsInfo is the persisted form of the registry.
type anchorsInfo struct {
	Anchors []anchorInfo `json:"anchors,omitempty"`
}

type anchorInfo struct {
	Branch   string `json:"branch"`
	Worktree string `json:"worktree,omitempty"`
	Base     string `json:"base,omitempty"`
}

// loadAnchors reads the registry into the in-memory map.
// A missing key is not an error: it yields an empty registry.
func (s *Store) loadAnchors(ctx context.Context) error {
	s.anchors = make(map[string]Anchor)

	var info anchorsInfo
	if err := s.db.Get(ctx, _anchorsJSON, &info); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil
		}
		return fmt.Errorf("get anchors: %w", err)
	}

	for _, a := range info.Anchors {
		s.anchors[a.Branch] = Anchor(a)
	}
	return nil
}

// IsTrunk reports whether the given branch is a trunk: either the
// canonical trunk or a registered anchor. Trunk branches are graph roots
// and may not be tracked as stack branches.
func (s *Store) IsTrunk(branch string) bool {
	if branch == s.trunk {
		return true
	}
	_, ok := s.anchors[branch]
	return ok
}

// TrunkFor returns the trunk branch that applies to the given worktree:
// the worktree's registered anchor if it has one, otherwise the canonical
// trunk. An empty worktreePath always resolves to the canonical trunk.
//
// RegisterAnchor enforces at most one anchor per worktree, but legacy
// state could hold more than one. Resolution iterates anchors in sorted
// branch order so the result is stable regardless of Go's randomized map
// iteration.
func (s *Store) TrunkFor(worktreePath string) string {
	if worktreePath != "" {
		for _, branch := range slices.Sorted(maps.Keys(s.anchors)) {
			if s.anchors[branch].Worktree == worktreePath {
				return branch
			}
		}
	}
	return s.trunk
}

// Anchors returns the registered anchors, sorted by branch name for
// stable output.
func (s *Store) Anchors() []Anchor {
	anchors := make([]Anchor, 0, len(s.anchors))
	for _, a := range s.anchors {
		anchors = append(anchors, a)
	}
	slices.SortFunc(anchors, func(a, b Anchor) int {
		return cmpString(a.Branch, b.Branch)
	})
	return anchors
}

// RegisterAnchor records a branch as the anchor for a worktree.
//
// It is idempotent for a given branch: re-registering an existing anchor
// updates its worktree path and base (so a relocated worktree can be
// re-pointed). It refuses to register a second, different anchor for a
// worktree that already has one, since each worktree resolves to exactly
// one trunk ([Store.TrunkFor]).
func (s *Store) RegisterAnchor(ctx context.Context, a Anchor) error {
	if a.Branch == "" {
		return errors.New("anchor branch name is required")
	}
	if a.Branch == s.trunk {
		return fmt.Errorf("branch %q is already the canonical trunk", a.Branch)
	}

	// Reject a second anchor for a worktree that already has a different
	// one. An empty worktree path is unaddressable, so it never conflicts.
	if a.Worktree != "" {
		for branch, existing := range s.anchors {
			if branch != a.Branch && existing.Worktree == a.Worktree {
				return fmt.Errorf(
					"worktree %q already has anchor %q", a.Worktree, branch)
			}
		}
	}

	next := make(map[string]Anchor, len(s.anchors)+1)
	maps.Copy(next, s.anchors)
	next[a.Branch] = a

	if err := s.saveAnchors(ctx, next,
		fmt.Sprintf("register anchor %q", a.Branch)); err != nil {
		return err
	}
	s.anchors = next
	return nil
}

// UnregisterAnchor removes a branch from the registry. Removing a branch
// that is not registered is a no-op.
func (s *Store) UnregisterAnchor(ctx context.Context, branch string) error {
	if _, ok := s.anchors[branch]; !ok {
		return nil
	}

	next := maps.Clone(s.anchors)
	delete(next, branch)

	if err := s.saveAnchors(ctx, next,
		fmt.Sprintf("unregister anchor %q", branch)); err != nil {
		return err
	}
	s.anchors = next
	return nil
}

// saveAnchors persists the registry map under the well-known key.
func (s *Store) saveAnchors(
	ctx context.Context, anchors map[string]Anchor, msg string,
) error {
	info := anchorsInfo{
		Anchors: make([]anchorInfo, 0, len(anchors)),
	}
	for _, a := range anchors {
		info.Anchors = append(info.Anchors, anchorInfo(a))
	}
	slices.SortFunc(info.Anchors, func(a, b anchorInfo) int {
		return cmpString(a.Branch, b.Branch)
	})

	if err := s.db.Update(ctx, storage.UpdateRequest{
		Sets:    []storage.SetRequest{{Key: _anchorsJSON, Value: info}},
		Message: msg,
	}); err != nil {
		return fmt.Errorf("update anchors: %w", err)
	}
	return nil
}

// cmpString is a tiny helper to avoid importing cmp just for one call.
func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
