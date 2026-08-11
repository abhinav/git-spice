package submit

import (
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/spice"
)

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
