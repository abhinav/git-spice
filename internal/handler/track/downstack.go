package track

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/ui"
)

// DownstackRequest is the request for tracking a branch
// and all its downstack branches.
type DownstackRequest struct {
	// Branch is the name of the branch to start tracking from.
	// We will walk down the commit history starting at this branch.
	// It is an error for this branch to already be tracked.
	Branch string // required
}

// TrackDownstack tracks all untracked branches in the downstack of a branch.
func (h *Handler) TrackDownstack(ctx context.Context, req *DownstackRequest) error {
	must.NotBeBlankf(req.Branch, "branch name must not be blank")

	log, store := h.Log, h.Store
	if req.Branch == store.Trunk() {
		return errors.New("cannot track trunk branch")
	}

	trackedBranches := make(map[string]struct{})
	for branch, err := range h.Store.ListBranches(ctx) {
		if err != nil {
			return fmt.Errorf("list tracked branches: %w", err)
		}
		trackedBranches[branch] = struct{}{}
	}

	// If the branch is already tracked, abort.
	if _, ok := trackedBranches[req.Branch]; ok {
		log.Infof("%v: branch is already tracked", req.Branch)
		return nil
	}

	discoverer, err := newDownstackDiscoverer(
		ctx, h.Log, h.Repository, store.Trunk(),
		&downstackDiscoveryView{Log: log, View: h.View},
		trackedBranches,
	)
	if err != nil {
		return fmt.Errorf("initialize discovery: %w", err)
	}

	// Discover all branches in the downstack.
	branchesToTrack, err := discoverer.Discover(ctx, req.Branch)
	if err != nil {
		return fmt.Errorf("discover downstack branches: %w", err)
	}

	// branchesToTrack cannot be empty because req.Branch is guaranteed
	// to be untracked.
	must.NotBeEmptyf(branchesToTrack, "no branches to track found in downstack of %v", req.Branch)

	// branchesToTrack is currently in top-down order
	// (from req.Branch down to the bottom).
	// We can only track them in bottom-up order
	// (trunk -> ... -> req.Branch),
	slices.Reverse(branchesToTrack)

	tx := store.BeginBranchTx()
	for _, branch := range branchesToTrack {
		if err := tx.Upsert(ctx, state.UpsertRequest{
			Name:     branch.name,
			Base:     branch.base,
			BaseHash: branch.baseHash,
		}); err != nil {
			return fmt.Errorf("track %v with base %v: %w", branch.name, branch.base, err)
		}

		log.Infof("%v: tracking with base %v", branch.name, branch.base)
	}

	msg := fmt.Sprintf("track downstack from %v (%d branches)", req.Branch, len(branchesToTrack))
	if err := tx.Commit(ctx, msg); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	log.Infof("%d branches added", len(branchesToTrack))
	return nil
}

// downstackDiscoverer traverses the commit graph downwards,
// discovering branches and their bases for tracking.
type downstackDiscoverer struct {
	log      *silog.Logger
	repo     GitRepository
	interact downstackDiscoveryInteraction

	// trunkHash is the hash of the trunk branch.
	// No discovery goes past this commit.
	trunkHash git.Hash
	trunk     string

	// branchesByHash maps commit hashes to branch names at that commit.
	// Multiple branches may exist at the same commit.
	branchesByHash map[git.Hash][]string

	// branchHashes maps branch names to their commit hashes.
	branchHashes map[string]git.Hash

	// trackedBranches is the set of branches
	// known to already be tracked by git-spice.
	//
	// If we use a branch in this set as a base for another branch,
	// we no longer need to continue discovery downstack from there.
	trackedBranches map[string]struct{}
}

//go:generate mockgen -destination=downstack_mocks_test.go -package=track -mock_names=downstackDiscoveryInteraction=MockDownstackDiscoveryInteraction -typed . downstackDiscoveryInteraction

func newDownstackDiscoverer(
	ctx context.Context,
	log *silog.Logger,
	repo GitRepository,
	trunk string,
	interact downstackDiscoveryInteraction,
	trackedBranches map[string]struct{},
) (*downstackDiscoverer, error) {
	branchesByHash := make(map[git.Hash][]string) // commit hash -> branch names
	branchHashes := make(map[string]git.Hash)
	for branch, err := range repo.LocalBranches(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list local branches: %w", err)
		}

		branchesByHash[branch.Hash] = append(branchesByHash[branch.Hash], branch.Name)
		branchHashes[branch.Name] = branch.Hash
	}

	trunkHash, ok := branchHashes[trunk]
	if !ok {
		return nil, fmt.Errorf("trunk branch %v does not exist", trunk)
	}

	return &downstackDiscoverer{
		log:             log,
		repo:            repo,
		trunk:           trunk,
		interact:        interact,
		trunkHash:       trunkHash,
		branchesByHash:  branchesByHash,
		branchHashes:    branchHashes,
		trackedBranches: trackedBranches,
	}, nil
}

// branchToTrack represents a branch that should be tracked
// with its base branch and base hash.
type branchToTrack struct {
	// Name of the branch to track.
	name string

	// Base branch to use when tracking.
	base     string
	baseHash git.Hash
}

// Discover discovers all branches downstack from the given branch
// and returns a list of branches to track in top-down order:
// first item is the given branch,
// last item is the bottom-most branch found,
// with a base that's either trunk or an already tracked branch.
//
// It walks commits from startHash down to stopHash,
// identifying untracked branches
// and prompting the user to select base branches as needed.
// In non-interactive mode, it stops proceeding
// if user input would be required.
func (d *downstackDiscoverer) Discover(
	ctx context.Context,
	branchName string,
) ([]branchToTrack, error) {
	var result []branchToTrack
	planned := make(map[string]struct{})

	startHash, ok := d.branchHashes[branchName]
	if !ok {
		return nil, fmt.Errorf("branch %v does not exist", branchName)
	}

	nextBranch := branchName
	commitRange := git.CommitRangeFrom(startHash).ExcludeFrom(d.trunkHash)
commitLoop:
	for commitHash, err := range d.repo.ListCommits(ctx, commitRange) {
		if err != nil {
			return nil, fmt.Errorf("list commits: %w", err)
		}

		// Find branches at this commit,
		// excluding the branch we're currently trying to find a base for
		// (which will be shown for the first commit only).
		var branchesHere []string
		for _, branch := range d.branchesByHash[commitHash] {
			if branch != nextBranch {
				branchesHere = append(branchesHere, branch)
			}
		}
		sort.Strings(branchesHere)

		if len(branchesHere) == 0 {
			// No branches at this commit.
			// Continue downstack.
			continue
		}

		// There is at least one branch at this commit.
		//
		// Ask the user to pick zero or more branches.
		// Outcomes:
		//
		//  - user requests skipping:
		//    continue downstack with the same nextBranch
		//
		//  - user selects a branch:
		//    selected branch is added as base for nextBranch,
		//    and the remaining branches are presented
		//    to become the base for that, and so on,
		//    until we run out of branches, or the user skips,
		//    or the user selects a branch that is already tracked.
		orderedBases, err := d.selectBranches(branchesHere, nextBranch, commitHash)
		if err != nil {
			return nil, fmt.Errorf("select base branch for %v at %v: %w",
				nextBranch, commitHash.Short(), err)
		}
		if len(orderedBases) == 0 {
			continue commitLoop
		}

		for _, base := range orderedBases {
			d.log.Debug("Adding branch to track", "branch", nextBranch, "base", base)
			result = append(result, branchToTrack{
				name:     nextBranch,
				base:     base,
				baseHash: commitHash,
			})
			planned[nextBranch] = struct{}{}

			// Check if we should stop discovery.
			if d.shouldStopDiscovery(base) {
				return result, nil
			}

			// Continue downstack with this base.
			nextBranch = base
		}
	}

	// If we started looking for a base for a branch (nextBranch)
	// but didn't find it (nextBranch not in planned),
	// it means we reached trunk without finding a base.
	// We must track this branch with trunk as the base.
	if _, ok := planned[nextBranch]; nextBranch != "" && !ok {
		result = append(result, branchToTrack{
			name:     nextBranch,
			base:     d.trunk,
			baseHash: d.trunkHash,
		})
	}

	return result, nil
}

// selectBranches prompts the user to pick
// from the given branches at commitHash
// to use as the base for the nextBranch.
//
// For >1 branches, the remaining branches are presented to the user
// to allow drawing relationships between multiple branches at the same commit.
//
// For each prompt, options are:
//
//   - user picks an untracked branch:
//     we'll use that as the next base, and continue prompting
//   - user selects a tracked branch or trunk:
//     we'll use that as the next base, and stop prompting
//   - user asks to skip:
//     return what we have so far
//
// Returns a (possibly empty) ordered list of branches to use as bases.
// The first branch in the list is the base for nextBranch,
// the second branch is the base for the first branch, etc.
//
// In non-interactive mode:
//   - if exactly one branch exists for any choice, it is selected automatically
//   - if multiple branches exist, we return an error
func (d *downstackDiscoverer) selectBranches(
	branches []string,
	nextBranch string,
	commitHash git.Hash,
) ([]string, error) {
	must.NotBeEmptyf(branches, "no branches to select from at commit %v for %v", commitHash.Short(), nextBranch)

	remaining := branches
	currentHead := nextBranch
	var ordered []string
	for len(remaining) > 0 {
		selected, err := d.interact.SelectBaseBranch(
			currentHead, commitHash, remaining, ordered,
		)
		if err != nil {
			return nil, fmt.Errorf("select base branch for %v at %v: %w",
				currentHead, commitHash.Short(), err)
		}

		// User asked to skip.
		// Break out and return what we have
		// to traverse downstack further.
		if selected == "" {
			break
		}

		ordered = append(ordered, selected)
		currentHead = selected
		remaining = slices.DeleteFunc(remaining, func(s string) bool {
			return s == selected
		})

		// If the selected branch is a termination branch
		// (tracked or trunk), stop prompting.
		if d.shouldStopDiscovery(selected) {
			break
		}
	}

	return ordered, nil
}

// shouldStopDiscovery determines whether discovery should terminate
// at the given branch--either because it's already tracked
// or because it's the trunk branch.
func (d *downstackDiscoverer) shouldStopDiscovery(branchName string) bool {
	if _, ok := d.trackedBranches[branchName]; ok {
		return true
	}

	if branchName == d.trunk {
		return true
	}

	return false
}

type downstackDiscoveryInteraction interface {
	// SelectBaseBranch prompts the user to select
	// a base branch for branchName at commitHash
	// from the list of candidateBranches.
	//
	// selectedSoFar contains branches already selected
	// in this downstack traversal, if any.
	//
	// Returns the selected branch name,
	// or an empty string if the user chose to skip.
	SelectBaseBranch(
		branchName string,
		commitHash git.Hash,
		candidateBranches []string,
		selectedSoFar []string,
	) (string, error)
}

type downstackDiscoveryView struct {
	Log  *silog.Logger // required
	View ui.View       // required
}

var _ downstackDiscoveryInteraction = (*downstackDiscoveryView)(nil)

func (v *downstackDiscoveryView) SelectBaseBranch(
	branchName string,
	commitHash git.Hash,
	candidateBranches []string,
	selectedSoFar []string,
) (string, error) {
	if !ui.Interactive(v.View) {
		if len(candidateBranches) > 1 {
			v.Log.Error("%v: multiple branches found at commit %v: %v",
				branchName, commitHash.Short(), strings.Join(candidateBranches, ", "))
			return "", ui.ErrPrompt
		}

		// Exactly one candidate.
		return candidateBranches[0], nil
	}

	var description strings.Builder
	fmt.Fprintf(&description, "Found branch %v at commit %v downstack from %v.\n", candidateBranches[0], commitHash.Short(), branchName)
	fmt.Fprintf(&description, "We can track it as the base for %v, or skip it.\n", branchName)
	if len(selectedSoFar) > 0 {
		description.WriteString("Branches already selected at this commit: ")
		for i, b := range selectedSoFar {
			if i > 0 {
				description.WriteString(" -> ")
			}
			description.WriteString(b)
		}
	}

	var selected *string
	prompt := ui.NewSelect[*string]().
		WithTitle(fmt.Sprintf("Track %v with base", branchName)).
		WithDescription(description.String()).
		WithValue(&selected).
		With(ui.OptionalComparableOptions("None of these", &candidateBranches[0], candidateBranches...))
	if err := ui.Run(v.View, prompt); err != nil {
		return "", fmt.Errorf("prompt for base branch: %w", err)
	}

	if selected == nil {
		return "", nil
	}

	return *selected, nil
}
