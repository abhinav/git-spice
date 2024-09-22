package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/maputil"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/spice/state/storage"
)

// ErrTrunk is returned when a trunk branch is used in a request
// that does not allow it.
var ErrTrunk = errors.New("trunk branch is not allowed")

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

// branchKey returns the path to the JSON file for the given branch
// relative to the store's root.
func branchKey(name string) string {
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
	if err := s.db.Get(ctx, branchKey(name), &state); err != nil {
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

// branchGraph is an acyclic directed graph of branches.
// Edges describe branch->base relationships.
type branchGraph struct {
	byName map[string]int

	trunk  string         // name of trunk
	names  []string       // name of branch[i]
	bases  []int          // index of branch[i].Base
	states []*branchState // state of branch[i]
	aboves [][]int        // branches with branch[i] as base, sorted
}

func loadGraph(ctx context.Context, s *Store) (*branchGraph, error) {
	names, err := s.ListBranches(ctx)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	// Always put trunk in the front.
	names = slices.Insert(names, 0, s.trunk)

	byName := make(map[string]int, len(names))
	states := make([]*branchState, len(names))
	for i, name := range names {
		if i == 0 {
			// no state for trunk
			byName[name] = i
			continue
		}

		state, err := s.lookupBranchState(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("get branch %q: %w", name, err)
		}

		byName[name] = i
		states[i] = state
	}

	bases := make([]int, len(names))
	aboves := make([][]int, len(names))
	for i, state := range states {
		if i == 0 {
			// no base for trunk
			bases[i] = -1
			continue
		}

		base := state.Base.Name
		must.NotBeBlankf(base, "branch %q has no base", names[i])

		baseIdx, ok := byName[base]
		if !ok {
			return nil, fmt.Errorf("branch %q has untracked base %q", names[i], base)
		}

		bases[i] = baseIdx
		aboves[baseIdx] = append(aboves[baseIdx], i)
	}
	for _, above := range aboves {
		sort.Ints(above)
	}

	return &branchGraph{
		byName: byName,
		names:  names,
		bases:  bases,
		states: states,
		aboves: aboves,
		trunk:  s.trunk,
	}, nil
}

// Path returns the path from 'from' to 'to' in the branch graph,
// or nil if there is no path.
//
// If the returned path is non-nil,
// the first element will always be 'from'
// and the last element will always be 'to'.
func (g *branchGraph) path(from, to int) []int {
	if from == 0 {
		// There will never be a path from trunk to any other branch.
		return nil
	}

	var p []int
	for cur := from; cur != to; cur = g.bases[cur] {
		if cur == -1 {
			return nil
		}
		p = append(p, cur)
	}
	return append(p, to)
}

// State returns the state of the branch with the given name
// or nil if the branch does not exist.
func (g *branchGraph) State(name string) *branchState {
	must.NotBeEqualf(name, g.trunk, "trunk has no state")
	idx, ok := g.byName[name]
	if !ok {
		return nil
	}
	return g.states[idx]
}

func (g *branchGraph) NewBranch(name string) *branchState {
	must.NotBeEqualf(name, g.trunk, "trunk has no state")
	idx, ok := g.byName[name]
	must.Bef(!ok || idx == -1, "branch %q already exists", name)

	idx = len(g.names)
	state := &branchState{}
	g.names = append(g.names, name)
	g.bases = append(g.bases, -1)
	g.states = append(g.states, state)
	g.aboves = append(g.aboves, nil)
	g.byName[name] = idx
	return state
}

func (g *branchGraph) SetBase(name, base string) error {
	// Base must already exist for name->base to be valid.
	baseIdx, ok := g.byName[base]
	if !ok || baseIdx == -1 {
		return &branchUntrackedError{Name: base}
	}

	nameIdx, ok := g.byName[name]
	if !ok || baseIdx == -1 {
		return fmt.Errorf("branch %q does not exist", name)
	}

	// Adding a name->base edge will not create a cycle
	// only if there's no existing path from base to name.
	if p := g.path(baseIdx, nameIdx); len(p) > 0 {
		path := make([]string, len(p))
		for i, idx := range p {
			path[i] = g.names[idx]
		}
		return newBranchCycleError(path)
	}

	if oldBaseIdx := g.bases[nameIdx]; oldBaseIdx != -1 && oldBaseIdx != baseIdx {
		// If the old base is not the same,
		// remove name from the old base's aboves.
		aboves := g.aboves[oldBaseIdx]
		if idx, ok := slices.BinarySearch(aboves, nameIdx); ok {
			aboves = slices.Delete(aboves, idx, idx+1)
			g.aboves[oldBaseIdx] = aboves
		}
	}

	g.bases[nameIdx] = baseIdx
	g.aboves[baseIdx] = append(g.aboves[baseIdx], nameIdx)
	return nil
}

func (g *branchGraph) DeleteBranch(name string) error {
	must.NotBeEqualf(name, g.trunk, "trunk cannot be deleted")

	idx, ok := g.byName[name]
	if !ok || idx == -1 {
		return fmt.Errorf("branch %q does not exist", name)
	}

	// Deletion will not cause a broken path
	// only if the branch is not a base for any other branch.
	if aboves := g.aboves[idx]; len(aboves) > 0 {
		var sb strings.Builder
		for i, idx := range aboves {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(g.names[idx])
		}
		return fmt.Errorf("branch %v is needed by %v", name, sb.String())
	}

	// So as not to break existing indexes,
	// just invalidate all information about the branch.
	g.byName[name] = -1
	g.names[idx] = ""
	g.bases[idx] = -1
	g.states[idx] = nil
	g.aboves[idx] = nil
	return nil
}

// UpdateBranch upates the store with the parameters in the request.
func (s *Store) UpdateBranch(ctx context.Context, req *UpdateRequest) error {
	if req.Message == "" {
		req.Message = fmt.Sprintf("update at %s", time.Now().Format(time.RFC3339))
	}

	graph, err := loadGraph(ctx, s)
	if err != nil {
		return fmt.Errorf("load branch graph: %w", err)
	}

	sets := make([]storage.SetRequest, 0, len(req.Upserts))
	for i, req := range req.Upserts {
		if req.Name == "" {
			return fmt.Errorf("upsert [%d]: branch name is required", i)
		}
		if req.Name == s.trunk {
			return fmt.Errorf("upsert [%d] (%q): %w", i, req.Name, ErrTrunk)
		}

		b := graph.State(req.Name)
		if b == nil {
			b = graph.NewBranch(req.Name)
		}

		if req.Base != "" {
			if err := graph.SetBase(req.Name, req.Base); err != nil {
				return fmt.Errorf("add branch %v with base %v: %w", req.Name, req.Base, err)
			}
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
			return fmt.Errorf("branch %q would have no base", req.Name)
		}

		sets = append(sets, storage.SetRequest{
			Key:   branchKey(req.Name),
			Value: b,
		})
	}

	deletes := make([]string, len(req.Deletes))
	for i, name := range req.Deletes {
		if name == s.trunk {
			return fmt.Errorf("delete [%d] (%q): %w", i, name, ErrTrunk)
		}

		if err := graph.DeleteBranch(name); err != nil {
			return fmt.Errorf("delete branch %v: %w", name, err)
		}

		deletes[i] = branchKey(name)
	}

	err = s.db.Update(ctx, storage.UpdateRequest{
		Sets:    sets,
		Deletes: deletes,
		Message: req.Message,
	})
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	return nil
}

type branchCycleError struct {
	path []string
}

// newBranchCycleError creates an error indicating that adding
// branch->base would create a cycle.
//
// path is the path from base to branch, inclusive.
func newBranchCycleError(path []string) error {
	must.NotBeEmptyf(path, "path is required")
	branch := path[len(path)-1]

	// The path is easier to visualize in reverse:
	// branch -> [..path...] -> base -> branch
	slices.Reverse(path) // branch -> ... -> base
	path = append(path, branch)

	return &branchCycleError{
		path: path,
	}
}

func (e *branchCycleError) Error() string {
	path := strings.Join(e.path, " -> ")
	return fmt.Sprintf("would create a cycle: %v", path)
}

type branchUntrackedError struct{ Name string }

func (e *branchUntrackedError) Error() string {
	return fmt.Sprintf("branch %v is not tracked", e.Name)
}
