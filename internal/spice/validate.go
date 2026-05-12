package spice

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// StaleBaseError indicates a branch whose base
// was merged on the forge but not yet rebased onto trunk.
type StaleBaseError struct {
	// Branch is the branch with the stale base.
	Branch string

	// Base is the merged base branch.
	Base string
}

func (e *StaleBaseError) Error() string {
	return fmt.Sprintf(
		"%s has stale base %s (already merged)",
		e.Branch, e.Base,
	)
}

// staleBaseCandidate is a branch whose base has a published
// change that needs forge state verification.
type staleBaseCandidate struct {
	branch   string
	base     string
	changeID forge.ChangeID
}

// ValidateDownstack checks that no branch in the downstack
// has a base whose forge change has already been merged.
// Returns a *StaleBaseError if a stale base is detected.
//
// If forgeRepo is nil (e.g. unsupported forge), validation is skipped.
func ValidateDownstack(
	ctx context.Context,
	graph *BranchGraph,
	forgeRepo forge.Repository,
	branch string,
) error {
	if forgeRepo == nil {
		return nil
	}
	candidates := collectStaleCandidates(graph, branch)
	if len(candidates) == 0 {
		return nil
	}
	return checkStaleStates(ctx, forgeRepo, candidates)
}

// collectStaleCandidates walks the downstack
// and returns branches whose base has a published change.
func collectStaleCandidates(
	graph *BranchGraph,
	branch string,
) []staleBaseCandidate {
	trunk := graph.Trunk()
	var candidates []staleBaseCandidate
	for name := range graph.Downstack(branch) {
		item, ok := graph.Lookup(name)
		if !ok || item.Base == trunk {
			continue
		}

		baseItem, ok := graph.Lookup(item.Base)
		if !ok || baseItem.Change == nil {
			continue
		}

		candidates = append(candidates, staleBaseCandidate{
			branch:   name,
			base:     item.Base,
			changeID: baseItem.Change.ChangeID(),
		})
	}
	return candidates
}

// checkStaleStates queries the forge for change states
// and returns a *StaleBaseError if any base is merged.
func checkStaleStates(
	ctx context.Context,
	forgeRepo forge.Repository,
	candidates []staleBaseCandidate,
) error {
	ids := make([]forge.ChangeID, len(candidates))
	for i, c := range candidates {
		ids[i] = c.changeID
	}

	statuses, err := forgeRepo.ChangeStatuses(ctx, ids)
	if err != nil {
		return fmt.Errorf("query change states: %w", err)
	}

	for i, c := range candidates {
		if statuses[i].State == forge.ChangeMerged {
			return &StaleBaseError{
				Branch: c.branch,
				Base:   c.base,
			}
		}
	}
	return nil
}
