package spice

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/must"
)

// StaleBase is a local branch/base edge
// whose base has already landed on the forge.
type StaleBase struct {
	// Branch is the local branch whose recorded base is obsolete.
	Branch string

	// Base is the local base branch whose change request was merged.
	Base string

	// ChangeID identifies Base's published change on the forge.
	ChangeID forge.ChangeID
}

// FindStaleBases reports local branch/base edges whose base branch has a
// published change request that is already merged on the forge.
func FindStaleBases(
	ctx context.Context,
	graph *BranchGraph,
	openForgeRepo func(context.Context) (forge.Repository, error),
	branches []string,
) ([]StaleBase, error) {
	candidates := staleBaseCandidates(graph, branches)
	if len(candidates) == 0 {
		return nil, nil
	}

	ids := make([]forge.ChangeID, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ChangeID
	}

	forgeRepo, err := openForgeRepo(ctx)
	if err != nil {
		return nil, err
	}

	statuses, err := forgeRepo.ChangeStatuses(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("query change states: %w", err)
	}
	must.BeEqualf(len(statuses), len(candidates),
		"query change states: got %d statuses for %d changes",
		len(statuses), len(candidates),
	)

	var staleBases []StaleBase
	for i, candidate := range candidates {
		if statuses[i].State == forge.ChangeMerged {
			staleBases = append(staleBases, StaleBase(candidate))
		}
	}
	return staleBases, nil
}

// staleBaseCandidate identifies one local branch/base edge
// whose base CR may already be merged on the forge.
type staleBaseCandidate struct {
	// Branch is the local branch whose base may be stale.
	Branch string

	// Base is the downstack branch used as Branch's base.
	Base string

	// ChangeID identifies Base's published change on the forge.
	ChangeID forge.ChangeID
}

func staleBaseCandidates(
	graph *BranchGraph,
	branches []string,
) []staleBaseCandidate {
	// Downstack yields the branch first,
	// followed by each non-trunk base branch.
	// Adjacent names therefore describe the local branch/base edges.
	var candidates []staleBaseCandidate
	for _, branch := range branches {
		var child string
		for base := range graph.Downstack(branch) {
			if child != "" {
				candidates = append(candidates, staleBaseCandidate{
					Branch: child,
					Base:   base,
					// ChangeID is filled below.
				})
			}
			child = base
		}
	}

	// Only base branches with published changes need forge checks.
	// Multiple submitted branches may share a downstack,
	// so de-duplicate by the local base branch name.
	seen := make(map[string]struct{})
	verified := candidates[:0]
	for _, candidate := range candidates {
		baseItem, ok := graph.Lookup(candidate.Base)
		if !ok || baseItem.Change == nil {
			continue
		}

		changeID := baseItem.Change.ChangeID()
		if _, ok := seen[candidate.Base]; ok {
			continue
		}
		seen[candidate.Base] = struct{}{}

		candidate.ChangeID = changeID
		verified = append(verified, candidate)
	}
	return verified
}
