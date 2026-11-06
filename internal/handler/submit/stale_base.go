package submit

import (
	"context"
	"fmt"

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

	staleBases, err := spice.FindStaleBases(ctx, graph, h.upstreamRepository, branches)
	if err != nil {
		return err
	}
	for _, staleBase := range staleBases {
		h.Log.Warn("Branch has stale base",
			"branch", staleBase.Branch,
			"base", staleBase.Base,
		)
	}
	if len(staleBases) > 0 {
		return fmt.Errorf(
			"%d branches with stale bases were found; "+
				"run 'gs repo sync' first, "+
				"or use --force to submit anyway",
			len(staleBases),
		)
	}
	return nil
}
