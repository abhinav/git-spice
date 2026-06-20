package state

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/spice/state/storage"
)

// _exclusiveJSON is the store key that holds the exclusive-mode manifest.
// Its presence is the exclusive-mode marker: while the key exists, the
// repository is parked and belongs to a single process until restore.
// It is additive and optional; repositories that have never been parked
// (and older binaries) simply have no entry.
const _exclusiveJSON = "exclusive"

// ParkedWorktree records a worktree that was removed when the repository
// entered exclusive mode, so that restore can re-create it.
type ParkedWorktree struct {
	// Path is the absolute filesystem path the worktree had.
	Path string

	// Branch is the branch that was checked out in the worktree.
	// Empty if the worktree was in detached-HEAD state.
	Branch string

	// Anchor is the anchor branch the worktree owned, if any.
	// Recorded for legibility; restore re-checks-out Branch, and the
	// anchor registration itself survives parking untouched.
	Anchor string
}

// exclusiveInfo is the persisted form of the manifest.
type exclusiveInfo struct {
	Worktrees []parkedWorktreeInfo `json:"worktrees,omitempty"`
}

type parkedWorktreeInfo struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Anchor string `json:"anchor,omitempty"`
}

// loadExclusive reads the manifest into memory. A missing key is not an
// error: it leaves the store out of exclusive mode.
func (s *Store) loadExclusive(ctx context.Context) error {
	s.exclusive = nil

	var info exclusiveInfo
	if err := s.db.Get(ctx, _exclusiveJSON, &info); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil
		}
		return fmt.Errorf("get exclusive: %w", err)
	}

	worktrees := make([]ParkedWorktree, 0, len(info.Worktrees))
	for _, w := range info.Worktrees {
		worktrees = append(worktrees, ParkedWorktree(w))
	}
	s.exclusive = &worktrees
	return nil
}

// InExclusiveMode reports whether the repository is currently parked.
func (s *Store) InExclusiveMode() bool {
	return s.exclusive != nil
}

// ParkedWorktrees returns the worktrees recorded in the exclusive-mode
// manifest. It returns nil when the repository is not in exclusive mode.
func (s *Store) ParkedWorktrees() []ParkedWorktree {
	if s.exclusive == nil {
		return nil
	}
	return *s.exclusive
}

// Park enters exclusive mode, recording the given worktrees in the
// durable manifest. Calling it again overwrites the manifest, which makes
// park resumable: a re-run after a crash simply re-records the (possibly
// already-removed) worktrees.
func (s *Store) Park(ctx context.Context, worktrees []ParkedWorktree) error {
	info := exclusiveInfo{
		Worktrees: make([]parkedWorktreeInfo, 0, len(worktrees)),
	}
	for _, w := range worktrees {
		info.Worktrees = append(info.Worktrees, parkedWorktreeInfo(w))
	}

	if err := s.db.Update(ctx, storage.UpdateRequest{
		Sets:    []storage.SetRequest{{Key: _exclusiveJSON, Value: info}},
		Message: "enter exclusive mode",
	}); err != nil {
		return fmt.Errorf("update exclusive: %w", err)
	}

	next := make([]ParkedWorktree, len(worktrees))
	copy(next, worktrees)
	s.exclusive = &next
	return nil
}

// Unpark leaves exclusive mode by deleting the manifest. It is a no-op
// when the repository is not parked, so restore is idempotent.
func (s *Store) Unpark(ctx context.Context) error {
	if s.exclusive == nil {
		return nil
	}
	if err := s.db.Delete(ctx, _exclusiveJSON, "leave exclusive mode"); err != nil {
		return fmt.Errorf("delete exclusive: %w", err)
	}
	s.exclusive = nil
	return nil
}
