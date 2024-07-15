package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"time"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/maputil"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/storage"
)

const _branchesDir = "branches"

type branchStateBase struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type branchUpstreamState struct {
	Branch string `json:"branch,omitempty"`
}

type branchChangeState struct {
	Forge  string
	Change json.RawMessage
}

func (bs *branchChangeState) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{bs.Forge: bs.Change})
}

func (bs *branchChangeState) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("unmarshal change state: %w", err)
	}
	if len(m) != 1 {
		got := maputil.Keys(m)
		return fmt.Errorf("expected 1 forge key, got %d: %v", len(got), got)
	}

	for forge, raw := range m {
		bs.Forge = forge
		bs.Change = raw
	}

	return nil
}

type branchState struct {
	Base     branchStateBase      `json:"base"`
	Upstream *branchUpstreamState `json:"upstream,omitempty"`
	Change   *branchChangeState   `json:"change,omitempty"`
}

// branchJSON returns the path to the JSON file for the given branch
// relative to the store's root.
func (s *Store) branchJSON(name string) string {
	return path.Join(_branchesDir, name)
}

// ErrNotExist indicates that a key that was expected to exist does not exist.
var ErrNotExist = storage.ErrNotExist

// LookupResponse is the response to a Lookup request.
type LookupResponse struct {
	// Base is the base branch configured
	// for the requested branch.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This may not match the current hash of the base branch.
	BaseHash git.Hash

	// ChangeMetadata holds the metadata for the published change.
	// This is forge-specific and must be deserialized by the forge.
	ChangeMetadata json.RawMessage

	// ChangeForge is the forge that the change was published to.
	ChangeForge string

	// UpstreamBranch is the name of the upstream branch
	// or an empty string if the branch is not tracking an upstream branch.
	UpstreamBranch string
}

// LookupBranch returns information about a tracked branch.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) LookupBranch(ctx context.Context, name string) (*LookupResponse, error) {
	state, err := s.lookupBranchState(ctx, name)
	if err != nil {
		return nil, err
	}

	res := &LookupResponse{
		Base:     state.Base.Name,
		BaseHash: git.Hash(state.Base.Hash),
	}

	if change := state.Change; change != nil {
		res.ChangeMetadata = change.Change
		res.ChangeForge = change.Forge
	}

	if upstream := state.Upstream; upstream != nil {
		res.UpstreamBranch = upstream.Branch
	}

	return res, nil
}

func (s *Store) lookupBranchState(ctx context.Context, name string) (*branchState, error) {
	var state branchState
	if err := s.db.Get(ctx, s.branchJSON(name), &state); err != nil {
		return nil, fmt.Errorf("get branch state: %w", err)
	}
	return &state, nil
}

// ListBranches reports the names of all tracked branches.
// The list is sorted in lexicographic order.
func (s *Store) ListBranches(ctx context.Context) ([]string, error) {
	branches, err := s.db.Keys(ctx, _branchesDir)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	sort.Strings(branches)
	return branches, nil
}

// UpdateRequest is a request to add, update, or delete information about branches.
type UpdateRequest struct {
	// Upserts are requests to add or update information about branches.
	Upserts []UpsertRequest

	// Deletes are requests to delete information about branches.
	Deletes []string

	// Message is a message specifying the reason for the update.
	// This will be persisted in the Git commit message.
	Message string
}

// UpsertRequest is a request to add or update information about a branch.
type UpsertRequest struct {
	// Name is the name of the branch.
	Name string

	// Base branch to update to.
	//
	// Leave empty to keep the current base.
	Base string

	// BaseHash is the last known hash of the base branch.
	// This is used to detect if the base branch has been updated.
	//
	// Leave empty to keep the current base hash.
	BaseHash git.Hash

	// ChangeMetadata is arbitrary, forge-specific metadata
	// recorded with the branch.
	//
	// Leave this unset to keep the current metadata.
	ChangeMetadata json.RawMessage

	// ChangeForge is the forge that recorded the change.
	//
	// If ChangeMetadata is set, this must also be set.
	ChangeForge string

	// UpstreamBranch is the name of the upstream branch to track.
	// Leave empty to stop tracking an upstream branch.
	UpstreamBranch string
}

// UpdateBranch upates the store with the parameters in the request.
func (s *Store) UpdateBranch(ctx context.Context, req *UpdateRequest) error {
	if req.Message == "" {
		req.Message = fmt.Sprintf("update at %s", time.Now().Format(time.RFC3339))
	}

	sets := make([]storage.SetRequest, 0, len(req.Upserts))
	for i, req := range req.Upserts {
		if req.Name == "" {
			return fmt.Errorf("upsert [%d]: branch name is required", i)
		}
		if req.Name == s.trunk {
			return fmt.Errorf("upsert [%d]: trunk branch (%q) is not allowed", i, req.Name)
		}

		b, err := s.lookupBranchState(ctx, req.Name)
		if err != nil {
			if !errors.Is(err, ErrNotExist) {
				return fmt.Errorf("get branch: %w", err)
			}
			// Branch does not exist yet.
			b = &branchState{}
		}

		if req.Base != "" {
			b.Base.Name = req.Base
		}
		if req.BaseHash != "" {
			b.Base.Hash = req.BaseHash.String()
		}

		if len(req.ChangeMetadata) > 0 {
			must.NotBeBlankf(req.ChangeForge, "change forge is required when change metadata is set")
			b.Change = &branchChangeState{
				Forge:  req.ChangeForge,
				Change: req.ChangeMetadata,
			}
		}

		if req.UpstreamBranch != "" {
			b.Upstream = &branchUpstreamState{
				Branch: req.UpstreamBranch,
			}
		}

		if b.Base.Name == "" {
			return fmt.Errorf("branch %q (%d) would have no base", req.Name, i)
		}

		sets = append(sets, storage.SetRequest{
			Key:   s.branchJSON(req.Name),
			Value: b,
		})
	}

	deletes := make([]string, len(req.Deletes))
	for i, name := range req.Deletes {
		deletes[i] = s.branchJSON(name)
	}

	err := s.db.Update(ctx, storage.UpdateRequest{
		Sets:    sets,
		Deletes: deletes,
		Message: req.Message,
	})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}
