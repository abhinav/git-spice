package sync

import (
	"context"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice"
)

// retargetCandidate is a branch whose forge change
// needs retargeting after a sync deletion.
type retargetCandidate struct {
	branch   string
	changeID forge.ChangeID
	newBase  string
}

// collectRetargetCandidates identifies branches
// that need forge retargeting after sync deletion.
//
// It examines pre-deletion branch state to find branches
// that survive deletion but have a base being deleted.
// For each, it resolves the nearest surviving ancestor
// as the new base.
func collectRetargetCandidates(
	deletions []branchDeletion,
	branchGraph *spice.BranchGraph,
) iter.Seq[retargetCandidate] {
	return func(yield func(retargetCandidate) bool) {
		deletedNames := make(map[string]struct{}, len(deletions))
		for _, d := range deletions {
			deletedNames[d.BranchName] = struct{}{}
		}

		for c := range branchGraph.All() {
			if _, deleted := deletedNames[c.Name]; deleted {
				continue
			}
			if c.Change == nil {
				continue
			}
			if _, baseDeleted := deletedNames[c.Base]; !baseDeleted {
				continue
			}

			if !yield(retargetCandidate{
				branch:   c.Name,
				changeID: c.Change.ChangeID(),
				newBase: branchGraph.NextBase(c.Name, func(branch string) bool {
					_, deleted := deletedNames[branch]
					return deleted
				}),
			}) {
				return
			}
		}
	}
}

// retargetUpstackChanges retargets forge changes
// for upstack branches surviving deletion.
func (h *Handler) retargetUpstackChanges(ctx context.Context, candidates iter.Seq[retargetCandidate]) {
	for c := range candidates {
		h.Log.Infof("%s: retargeting %v onto %s...",
			c.branch, c.changeID, c.newBase)
		err := h.RemoteRepository.EditChange(
			ctx, c.changeID,
			forge.EditChangeOptions{Base: c.newBase},
		)
		if err != nil {
			h.Log.Warn("Retarget failed",
				"branch", c.branch, "error", err)
		}
	}
}
