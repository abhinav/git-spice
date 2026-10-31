package state

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"iter"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/sliceutil"
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
	Branch string `json:"branch,omitzero"`
}

type branchChangeState struct {
	Forge  string
	Change jsontext.Value
}

func (bs *branchChangeState) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{bs.Forge: bs.Change})
}

func (bs *branchChangeState) UnmarshalJSON(data []byte) error {
	var m map[string]jsontext.Value
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("unmarshal change state: %w", err)
	}
	if len(m) != 1 {
		got := slices.Sorted(maps.Keys(m))
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
	Upstream *branchUpstreamState `json:"upstream,omitzero"`
	Change   *branchChangeState   `json:"change,omitzero"`

	MergedDownstack []jsontext.Value `json:"merged,omitempty"`
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
	ChangeMetadata jsontext.Value

	// ChangeForge is the forge that the change was published to.
	ChangeForge string

	// UpstreamBranch is the name of the upstream branch
	// or an empty string if the branch is not tracking an upstream branch.
	UpstreamBranch string

	// MergedDownstack holds information about branches
	// that were previously downstack from this branch
	// that have since been merged into trunk.
	//
	// MergedDownstack is in the order that the branches were merged.
	// For example, if the stack was main -> A -> B -> C,
	// where C is this branch, MergedDownstack will be [A, B].
	MergedDownstack []jsontext.Value
}

// LookupBranch returns information about a tracked branch.
// If the branch is not found, [ErrNotExist] will be returned.
func (s *Store) LookupBranch(ctx context.Context, name string) (*LookupResponse, error) {
	responses, err := s.LookupBranches(ctx, []string{name})
	if err != nil {
		return nil, err
	}

	return responses[0], nil
}

// LookupBranches returns information about tracked branches.
// The returned slice matches the order of names.
//
// Missing branches have nil responses and are identified by the indexes in
// [storage.MissingKeysError]. All other responses are valid in that case.
// Other errors return no responses.
func (s *Store) LookupBranches(
	ctx context.Context,
	names []string,
) ([]*LookupResponse, error) {
	states, err := s.lookupBranchStates(ctx, names)
	if states == nil {
		return nil, err
	}

	responses := make([]*LookupResponse, len(states))
	for i, state := range states {
		if state != nil {
			responses[i] = branchStateLookupResponse(state)
		}
	}
	return responses, err
}

func branchStateLookupResponse(state *branchState) *LookupResponse {
	res := &LookupResponse{
		Base:            state.Base.Name,
		BaseHash:        git.Hash(state.Base.Hash),
		MergedDownstack: state.MergedDownstack,
	}

	if change := state.Change; change != nil {
		res.ChangeMetadata = change.Change
		res.ChangeForge = change.Forge
	}

	if upstream := state.Upstream; upstream != nil {
		res.UpstreamBranch = upstream.Branch
	}

	return res
}

// lookupBranchStates returns raw persisted states in request order.
// Missing entries are nil with a [storage.MissingKeysError]; other errors return
// no states, so callers never cache incomplete reads.
func (s *Store) lookupBranchStates(ctx context.Context, names []string) ([]*branchState, error) {
	if len(names) == 0 {
		return nil, nil
	}
	values := make([]branchState, len(names))
	states := make([]*branchState, len(names))
	reqs := make([]storage.GetRequest, len(names))
	for i, name := range names {
		states[i] = &values[i]
		reqs[i] = storage.GetRequest{Key: branchKey(name), Value: states[i]}
	}
	err := s.db.GetMany(ctx, reqs)
	if err == nil {
		return states, nil
	}
	missing, ok := errors.AsType[*storage.MissingKeysError](err)
	if !ok {
		return nil, fmt.Errorf("load branches: %w", err)
	}
	for _, index := range missing.Indexes {
		states[index] = nil
	}
	return states, fmt.Errorf("load branches: %w", err)
}

// ListBranches reports the names of all tracked branches.
// The list is sorted in lexicographic order.
func (s *Store) ListBranches(ctx context.Context) iter.Seq2[string, error] {
	return s.listBranches(ctx)
}

// listBranches returns the names of all branches in the store.
// The list is in no particular order.
func (s *Store) listBranches(ctx context.Context) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		branches, err := s.db.Keys(ctx, _branchesDir)
		if err != nil {
			yield("", err)
			return
		}

		for _, branch := range branches {
			if !yield(branch, nil) {
				return
			}
		}
	}
}

// BranchTx is an ongoing change to the branch graph.
// Changes made to it are not persisted until Commit is called.
// However, in-flight changes are visible to the transaction,
// so it can use them to prevent cycles and other issues.
type BranchTx struct {
	store *Store

	states  map[string]*branchState // cached states and pending changes; nil means absent
	sets    map[string]struct{}     // branches to set
	dels    map[string]struct{}     // branches to delete
	renames map[string]string       // old branch name to new branch name
}

// BeginBranchTx starts a new transaction for updating the branch graph.
// Changes are not persisted until Commit is called.
func (s *Store) BeginBranchTx() *BranchTx {
	return &BranchTx{
		store:   s,
		states:  make(map[string]*branchState),
		sets:    make(map[string]struct{}),
		dels:    make(map[string]struct{}),
		renames: make(map[string]string),
	}
}

// Null is a JSON null value.
var Null = jsontext.Value("null")

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
	// Use Null to clear the current metadata.
	//
	// Leave this unset to keep the current metadata.
	ChangeMetadata jsontext.Value

	// ChangeForge is the forge that recorded the change.
	//
	// If ChangeMetadata is set and not Null, this must be set.
	ChangeForge string

	// UpstreamBranch is the name of the upstream branch to track.
	// Leave nil to leave it unchanged, or set to an empty string to clear it.
	UpstreamBranch *string

	// MergedDownstack is a list of branches that were previously
	// downstack from this branch that have since been merged into trunk.
	MergedDownstack *[]jsontext.Value
}

// Upsert adds or updates information about a branch.
// If the branch is not known, it will be added.
// For new branches, a base MUST be provided.
func (tx *BranchTx) Upsert(ctx context.Context, req UpsertRequest) error {
	return tx.UpsertMany(ctx, []UpsertRequest{req})
}

// UpsertMany applies requests in order after loading their uncached branches
// and direct bases together. A request may use a branch created earlier in reqs.
// It stops at the first invalid request; preceding changes remain in the
// transaction, but nothing is persisted until Commit.
func (tx *BranchTx) UpsertMany(ctx context.Context, reqs []UpsertRequest) error {
	names := make([]string, 0, 2*len(reqs))
	for _, req := range reqs {
		switch req.Name {
		case "", tx.store.trunk:
		default:
			names = append(names, req.Name)
		}

		switch req.Base {
		case "", tx.store.trunk:
		default:
			names = append(names, req.Base)
		}
	}
	if err := tx.loadStates(ctx, names); err != nil {
		return err
	}
	for _, req := range reqs {
		if err := tx.upsert(ctx, req); err != nil {
			return fmt.Errorf("update branch %q: %w", req.Name, err)
		}
	}
	return nil
}

func (tx *BranchTx) upsert(ctx context.Context, req UpsertRequest) error {
	must.NotBeBlankf(req.Name, "branch name is required")
	must.NotBeEqualf(req.Name, tx.store.trunk, "branch name cannot be trunk")

	state, err := tx.state(ctx, req.Name)
	if err != nil {
		if !errors.Is(err, ErrNotExist) {
			return err
		}

		if req.Base == "" {
			return errors.New("new branch must have a base")
		}

		state = &branchState{Base: branchStateBase{Name: req.Base}}
		// Note:
		// Don't persist the state here until the rest
		// of the request is validated.
	}

	if req.Base != "" {
		if req.Base != tx.store.trunk {
			// Base must already be tracked for name->base to be valid.
			if _, err := tx.state(ctx, req.Base); err != nil {
				if errors.Is(err, ErrNotExist) {
					return &branchUntrackedError{Name: req.Base}
				}
				return fmt.Errorf("load base %q: %w", req.Base, err)
			}

			// Adding name->base will not create a cycle
			// only if there's no existing path from base to name.
			if path, err := tx.path(ctx, req.Base, req.Name); err != nil {
				return fmt.Errorf("find path from trunk to %q: %w", req.Name, err)
			} else if len(path) > 0 {
				return newBranchCycleError(path)
			}

		}
		state.Base.Name = req.Base
	}

	if req.BaseHash != "" {
		state.Base.Hash = req.BaseHash.String()
	}

	if len(req.ChangeMetadata) > 0 {
		if bytes.Equal(req.ChangeMetadata, Null) {
			state.Change = nil
		} else {
			must.NotBeBlankf(req.ChangeForge, "change forge is required when change metadata is set")
			state.Change = &branchChangeState{
				Forge:  req.ChangeForge,
				Change: req.ChangeMetadata,
			}
		}
	}

	if req.UpstreamBranch != nil {
		if *req.UpstreamBranch == "" {
			state.Upstream = nil
		} else {
			state.Upstream = &branchUpstreamState{
				Branch: *req.UpstreamBranch,
			}
		}
	}

	if req.MergedDownstack != nil {
		state.MergedDownstack = *req.MergedDownstack
	}

	tx.states[req.Name] = state
	tx.sets[req.Name] = struct{}{}
	delete(tx.dels, req.Name)
	return nil
}

// Delete removes information about a branch from the store.
//
// The branch must not be a base for any other branch,
// or an error will be returned.
func (tx *BranchTx) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("branch name is required")
	}
	if name == tx.store.trunk {
		return ErrTrunk
	}

	if _, err := tx.state(ctx, name); err != nil {
		return err
	}

	aboves, err := sliceutil.CollectErr(tx.aboves(ctx, name))
	if err != nil {
		return fmt.Errorf("list branches above %v: %w", name, err)
	}
	if len(aboves) > 0 {
		return fmt.Errorf("branch %v is needed by %v", name, strings.Join(aboves, ", "))
	}

	tx.dels[name] = struct{}{}
	delete(tx.sets, name)
	delete(tx.states, name)
	return nil
}

// Rename changes a tracked branch name while preserving its stored state.
// Commit also moves state owned by the branch, such as review drafts.
func (tx *BranchTx) Rename(
	ctx context.Context,
	oldName, newName string,
) error {
	if oldName == "" || newName == "" {
		return errors.New("old and new branch names are required")
	}
	if oldName == tx.store.trunk || newName == tx.store.trunk {
		return ErrTrunk
	}

	oldState, err := tx.state(ctx, oldName)
	if err != nil {
		return err
	}
	if _, err := tx.state(ctx, newName); err == nil {
		return fmt.Errorf("branch %v is already tracked", newName)
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}

	newState := *oldState
	tx.states[newName] = &newState
	delete(tx.states, oldName)
	tx.dels[oldName] = struct{}{}

	if _, modified := tx.sets[oldName]; modified {
		// The persisted source does not include earlier transaction changes.
		// Write the changed state under its new name instead of moving it.
		tx.sets[newName] = struct{}{}
	} else {
		tx.renames[oldName] = newName
	}
	delete(tx.sets, oldName)
	return nil
}

// Commit persists all planned changes to the store.
// If there are no changes, this is a no-op.
func (tx *BranchTx) Commit(ctx context.Context, msg string) error {
	req := updateBranchesRequest{
		Sets:    make([]setBranchStateRequest, 0, len(tx.sets)),
		Deletes: slices.Collect(maps.Keys(tx.dels)),
		Renames: maps.Clone(tx.renames),
		Message: msg,
	}

	for branch := range tx.sets {
		state, ok := tx.states[branch]
		must.Bef(ok, "branch %q is set but has no state", branch)
		req.Sets = append(req.Sets, setBranchStateRequest{
			Branch: branch,
			State:  state,
		})
	}

	if err := tx.store.updateBranches(ctx, req); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	clear(tx.sets)
	clear(tx.dels)
	clear(tx.states)
	clear(tx.renames)
	return nil
}

func (tx *BranchTx) state(ctx context.Context, branch string) (*branchState, error) {
	if err := tx.loadStates(ctx, []string{branch}); err != nil {
		return nil, err
	}
	state := tx.states[branch]
	if state == nil {
		return nil, ErrNotExist
	}
	return state, nil
}

// loadStates fills the cache for names not already loaded, changed, or deleted.
// Missing persisted branches are cached as nil so a later upsert can create them.
func (tx *BranchTx) loadStates(ctx context.Context, names []string) error {
	var pending []string
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := tx.states[name]; ok {
			continue
		}
		if _, deleted := tx.dels[name]; deleted {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		pending = append(pending, name)
	}
	if len(pending) == 0 {
		return nil
	}

	states, err := tx.store.lookupBranchStates(ctx, pending)
	if states == nil {
		return err
	}
	for i, name := range pending {
		tx.states[name] = states[i]
	}
	return nil
}

func (tx *BranchTx) listBranches(ctx context.Context) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		// seen prevents underlying branches from being listed twice.
		seen := make(map[string]struct{})

		// A renamed branch is visible under its destination name even when
		// its persisted state can move without being rewritten.
		for _, branch := range tx.renames {
			if _, deleted := tx.dels[branch]; deleted {
				continue
			}
			if !yield(branch, nil) {
				return
			}
			seen[branch] = struct{}{}
		}

		// Entries in tx.sets take precedence unless they are deleted.
		for branch := range tx.sets {
			if _, ok := tx.dels[branch]; ok {
				continue
			}
			if _, ok := seen[branch]; ok {
				continue
			}

			if !yield(branch, nil) {
				return
			}
			seen[branch] = struct{}{}
		}

		// List underlying branches.
		for branch, err := range tx.store.listBranches(ctx) {
			if err != nil {
				yield("", fmt.Errorf("list branches: %w", err))
				return
			}

			_, overridden := seen[branch]
			_, deleted := tx.dels[branch]
			if overridden || deleted {
				continue
			}

			if !yield(branch, err) {
				return
			}
			seen[branch] = struct{}{}
		}
	}
}

func (tx *BranchTx) aboves(ctx context.Context, branch string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		names, err := sliceutil.CollectErr(tx.listBranches(ctx))
		if err != nil {
			yield("", err)
			return
		}
		if err := tx.loadStates(ctx, names); err != nil {
			yield("", err)
			return
		}

		for _, branchName := range names {
			state, err := tx.state(ctx, branchName)
			if err != nil {
				yield("", fmt.Errorf("load branch %q: %w", branchName, err))
				return
			}

			if state.Base.Name == branch {
				if !yield(branchName, nil) {
					return
				}
			}
		}
	}
}

func (tx *BranchTx) path(ctx context.Context, from, to string) ([]string, error) {
	seen := make(map[string]struct{})
	var p []string
	for cur := from; cur != to; {
		if cur == tx.store.trunk {
			// There can never be a path from trunk to any other branch.
			return nil, nil
		}

		// We avoid state corruption by checking for cycles at add time.
		// If we see a cycle here, the state is already corrupt.
		// This is a bug and not recoverable.
		if _, ok := seen[cur]; ok {
			panic(fmt.Sprintf("corrupt store: cycle detected in branch graph: %v", append(p, cur)))
		}
		seen[cur] = struct{}{}

		state, err := tx.state(ctx, cur)
		if err != nil {
			return nil, fmt.Errorf("load branch %q: %w", cur, err)
		}

		p = append(p, cur)
		must.NotBeBlankf(state.Base.Name, "branch %q has no base", cur)
		cur = state.Base.Name
	}

	return append(p, to), nil
}

// setBranchStateRequest is a request to set the state of a branch.
type setBranchStateRequest struct {
	Branch string
	State  *branchState
}

// updateBranchesRequest is a request to update the state of multiple branches.
// The request can set, delete, or rename branches in one transaction.
// A message is recorded with the update.
type updateBranchesRequest struct {
	Sets    []setBranchStateRequest
	Deletes []string
	Renames map[string]string
	Message string // required
}

// updateBranches atomically updates the state of multiple branches in the store.
func (s *Store) updateBranches(ctx context.Context, req updateBranchesRequest) error {
	if len(req.Sets) == 0 && len(req.Deletes) == 0 && len(req.Renames) == 0 {
		return nil
	}

	if req.Message == "" {
		req.Message = "update at " + time.Now().Format(time.RFC3339)
	}

	sets := make([]storage.SetRequest, len(req.Sets))
	for idx, set := range req.Sets {
		sets[idx] = storage.SetRequest{
			Key:   branchKey(set.Branch),
			Value: set.State,
		}
	}

	var dels []string
	for _, del := range req.Deletes {
		if _, renamed := req.Renames[del]; !renamed {
			dels = append(
				dels,
				branchKey(del),
				reviewDraftsJSON(del),
			)
		}
	}

	var moves []storage.MoveRequest
	for _, oldName := range slices.Sorted(maps.Keys(req.Renames)) {
		newName := req.Renames[oldName]
		moves = append(
			moves,
			storage.MoveRequest{
				From: branchKey(oldName),
				To:   branchKey(newName),
			},
			storage.MoveRequest{
				From: reviewDraftsJSON(oldName),
				To:   reviewDraftsJSON(newName),
			},
		)
	}

	updReq := storage.UpdateRequest{
		Sets:    sets,
		Moves:   moves,
		Deletes: dels,
		Message: req.Message,
	}
	if err := s.db.Update(ctx, updReq); err != nil {
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
