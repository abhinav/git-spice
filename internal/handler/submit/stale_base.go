package submit

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
)

// checkStaleSubmissionBases prevents submit from acting on a stack whose local
// base relationships are already obsolete on the forge.
//
// The submit path may push branches or edit CR bases before per-branch submit
// logic discovers that a downstack base was merged externally. This preflight
// checks every submitted branch's downstack first so the user can run
// 'gs repo sync' before any remote state is changed.
func (h *Handler) checkStaleSubmissionBases(
	ctx context.Context,
	graph *spice.BranchGraph,
	branches []string,
	opts *Options,
) error {
	if opts.Force {
		return nil
	}

	candidates := staleBaseCandidates(graph, branches)
	if len(candidates) == 0 {
		return nil
	}

	remoteRepo, err := h.upstreamRepository(ctx)
	if err != nil {
		return fmt.Errorf("open remote repository: %w", err)
	}

	count, err := validateStaleBaseCandidates(
		ctx, remoteRepo, h.Log, candidates,
	)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf(
			"%d branches with stale bases were found; "+
				"run 'gs repo sync' first, "+
				"or use --force to submit anyway",
			count,
		)
	}
	return nil
}

// staleBaseCandidate identifies one local branch/base edge
// whose base CR may already be merged on the forge.
type staleBaseCandidate struct {
	// Branch is the Branch whose base may be stale.
	Branch string

	// Base is the downstack branch used as branch's Base.
	Base string

	// ChangeID identifies base's published change on the forge.
	ChangeID forge.ChangeID
}

// staleBaseCandidates finds local branch/base edges
// that need forge status checks before submit.
//
// It walks each submitted branch's downstack,
// keeps only edges whose base branch has published change metadata,
// and de-duplicates shared downstacks by local base branch name.
func staleBaseCandidates(
	graph *spice.BranchGraph,
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
					// changeID filled below.
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

// validateStaleBaseCandidates checks candidate base CRs on the forge,
// logs a warning for each merged base it finds,
// and returns the number of stale local branch/base edges.
//
// The returned count is separated from transport or forge query errors
// so callers can report one aggregate submit-blocking error
// after all stale edges have been logged.
func validateStaleBaseCandidates(
	ctx context.Context,
	forgeRepo forge.Repository,
	log *silog.Logger,
	candidates []staleBaseCandidate,
) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}

	ids := make([]forge.ChangeID, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ChangeID
	}

	statuses, err := forgeRepo.ChangeStatuses(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("query change states: %w", err)
	}
	must.BeEqualf(len(statuses), len(candidates),
		"query change states: got %d statuses for %d changes",
		len(statuses), len(candidates),
	)

	var count int
	for i, c := range candidates {
		if statuses[i].State == forge.ChangeMerged {
			log.Warn("Branch has stale base",
				"branch", c.Branch,
				"base", c.Base,
			)
			count++
		}
	}
	return count, nil
}
