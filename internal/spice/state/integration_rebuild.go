package state

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/git"
)

const _integrationRebuildJSON = "integration-rebuild"

type integrationRebuildState struct {
	Integration  string                  `json:"integration"`
	Tips         []integrationRebuildTip `json:"tips"`
	NextTipIndex int                     `json:"nextTipIndex"`
}

type integrationRebuildTip struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// IntegrationRebuild describes an in-progress integration rebuild that
// was paused (typically due to a merge conflict). Its presence signals
// that the next [gs integration rebuild] invocation should resume
// rather than start fresh.
type IntegrationRebuild struct {
	// Integration is the local name of the integration branch.
	Integration string

	// Tips holds the full set of tips for the rebuild, with the hashes
	// captured at the start of the original attempt. Indexed positions
	// align with [NextTipIndex].
	Tips []IntegrationTip

	// NextTipIndex points to the first tip that has not yet been
	// started. Tips with index < NextTipIndex have been merged
	// successfully (or the merge is in progress in the worktree, in
	// the case of the most recently attempted one).
	NextTipIndex int
}

// PendingIntegrationRebuild returns the saved pending rebuild, or
// [ErrNotExist] if no rebuild is in progress.
func (s *Store) PendingIntegrationRebuild(ctx context.Context) (*IntegrationRebuild, error) {
	var st integrationRebuildState
	if err := s.db.Get(ctx, _integrationRebuildJSON, &st); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, ErrNotExist
		}
		return nil, fmt.Errorf("get pending rebuild: %w", err)
	}

	tips := make([]IntegrationTip, len(st.Tips))
	for i, t := range st.Tips {
		tips[i] = IntegrationTip{Name: t.Name, Hash: git.Hash(t.Hash)}
	}
	return &IntegrationRebuild{
		Integration:  st.Integration,
		Tips:         tips,
		NextTipIndex: st.NextTipIndex,
	}, nil
}

// SetPendingIntegrationRebuild records a pending integration rebuild.
func (s *Store) SetPendingIntegrationRebuild(ctx context.Context, rb *IntegrationRebuild) error {
	tips := make([]integrationRebuildTip, len(rb.Tips))
	for i, t := range rb.Tips {
		tips[i] = integrationRebuildTip{Name: t.Name, Hash: t.Hash.String()}
	}
	st := integrationRebuildState{
		Integration:  rb.Integration,
		Tips:         tips,
		NextTipIndex: rb.NextTipIndex,
	}
	if err := s.db.Set(ctx, _integrationRebuildJSON, st, "save pending integration rebuild"); err != nil {
		return fmt.Errorf("save pending rebuild: %w", err)
	}
	return nil
}

// ClearPendingIntegrationRebuild removes any saved pending rebuild.
// No-op if none is saved.
func (s *Store) ClearPendingIntegrationRebuild(ctx context.Context) error {
	if err := s.db.Delete(ctx, _integrationRebuildJSON, "clear pending integration rebuild"); err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil
		}
		return fmt.Errorf("clear pending rebuild: %w", err)
	}
	return nil
}
