package submit

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice"
)

// updateStacks plans and executes the forge-native representation of the
// tracked stack trees affected by a successful submit. Unsupported planning
// applies the provider base edits deferred by the submit loop.
func (h *Handler) updateStacks(
	ctx context.Context,
	submitted []string,
	stackUpdates *submitStackUpdates,
) error {
	repo, err := h.upstreamRepository(ctx)
	if err != nil {
		return fmt.Errorf("get remote repository: %w", err)
	}

	stackRepo, ok := repo.(forge.StackRepository)
	if !ok {
		return nil
	}

	// Submission can create change metadata after the command builds its first
	// branch graph.
	// Reload it so newly published changes participate in the update.
	graph, err := h.Service.BranchGraph(ctx, nil)
	if err != nil {
		return fmt.Errorf("build branch graph: %w", err)
	}

	changes := nativeStackChanges(graph, repo.Forge().ID(), submitted)
	if len(changes) == 0 {
		return nil
	}

	plan, err := stackRepo.PlanStackUpdate(ctx, changes)
	if errors.Is(err, forge.ErrUnsupported) {
		return stackUpdates.applyDeferredBases(ctx)
	}
	if err != nil {
		h.Log.Warn("Could not plan stack update", "error", err)
		return nil
	}
	if err := plan.Execute(ctx); err != nil {
		h.Log.Warn("Could not update stacks", "error", err)
	}
	return nil
}

type submitStackUpdates struct {
	deferredBases []deferredBaseUpdate
}

type deferredBaseUpdate struct {
	repository forge.Repository
	change     forge.ChangeID
	base       string
}

func (u *submitStackUpdates) applyDeferredBases(ctx context.Context) error {
	var errs []error
	for _, update := range u.deferredBases {
		if err := update.repository.EditChange(ctx, update.change, forge.EditChangeOptions{
			Base: update.base,
		}); err != nil {
			errs = append(errs, fmt.Errorf("update %v base: %w", update.change, err))
		}
	}
	return errors.Join(errs...)
}

// nativeStackChanges projects every published change in a tree containing a
// submitted branch into the forge's native-stack representation.
//
// A submission may affect any branch in the same tree: adding or updating one
// change can complete a relationship elsewhere in its divergent upstack. The
// projection therefore starts at each submitted branch's bottom and retains
// all published changes for the target forge. If a branch's base is absent
// from that projection, the forge contract treats the branch as a tree root.
func nativeStackChanges(
	graph *spice.BranchGraph,
	forgeID string,
	submitted []string,
) []forge.StackChange {
	affectedBranches := make(map[string]struct{})
	for _, branch := range submitted {
		for member := range graph.Upstack(graph.Bottom(branch)) {
			affectedBranches[member] = struct{}{}
		}
	}

	changeByBranch := make(map[string]forge.ChangeID, len(affectedBranches))
	for branch := range graph.All() {
		if _, ok := affectedBranches[branch.Name]; !ok || branch.Change == nil {
			continue
		}
		if branch.Change.ForgeID() == forgeID {
			changeByBranch[branch.Name] = branch.Change.ChangeID()
		}
	}

	changes := make([]forge.StackChange, 0, len(changeByBranch))
	for branch := range graph.All() {
		change, ok := changeByBranch[branch.Name]
		if !ok {
			continue
		}
		baseBranch := branch.Base
		if base, ok := graph.Lookup(branch.Base); ok && base.UpstreamBranch != "" {
			baseBranch = base.UpstreamBranch
		}
		changes = append(changes, forge.StackChange{
			Change:     change,
			BaseChange: changeByBranch[branch.Base],
			BaseBranch: baseBranch,
		})
	}
	return changes
}
