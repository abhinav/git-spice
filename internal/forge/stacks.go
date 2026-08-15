package forge

import "context"

// StackRepository provides optional support
// for reconciling and merging forge-native stacks.
type StackRepository interface {
	Repository

	// PlanStackUpdate prepares a provider update for the supplied relationships.
	// Each entry identifies a change and its immediate base
	// when that base is also in the request.
	// BaseBranch identifies the provider-facing branch
	// even when BaseChange is absent from the request.
	//
	// Planning is read-only, including on failure.
	// If planning returns [ErrUnsupported],
	// the caller may apply its ordinary per-change base updates.
	//
	// Provider-specific tree limits do not make the capability unsupported.
	// Implementations may warn and omit those trees from the returned plan.
	PlanStackUpdate(context.Context, []StackChange) (StackUpdatePlan, error)

	// PlanMergeRanges identifies the supplied changes that the provider can
	// merge atomically using its current native-stack representation.
	// Returned plans are non-empty, disjoint subsets of changes.
	// Each plan follows the supplied relationships from bottom to top;
	// changes omitted from every plan remain eligible for ordinary per-change
	// merging.
	//
	// [ErrUnsupported] means that no plans were created and the caller may merge
	// every supplied change individually.
	PlanMergeRanges(context.Context, []MergePlanChange) ([]MergeRangePlan, error)
}

// StackUpdatePlan owns one prepared provider stack transition.
type StackUpdatePlan interface {
	// Execute applies the transition prepared by PlanStackUpdate.
	//
	// Execute may update independent trees before returning an error.
	// It never returns [ErrUnsupported].
	// Callers must prepare a new plan before retrying, regardless of the result.
	Execute(context.Context) error
}

// MergePlanChange identifies a selected change and its immediate base
// for merge planning.
type MergePlanChange struct {
	// Change identifies the selected change.
	Change ChangeID // required

	// BaseChange identifies the immediate base change
	// when that change is also selected for merging.
	// It may be nil for a root of the selected forest.
	BaseChange ChangeID
}

// MergeRangePlan identifies one provider-selected atomic merge range and owns
// the operation that can merge it.
type MergeRangePlan interface {
	// Changes identifies this range from bottom to top.
	// The caller must not modify the returned slice.
	Changes() []ChangeID

	// Merge atomically merges this planned range.
	// The request supplies the expected head commit of each planned change
	// after preparation; its order does not affect the plan's merge order.
	//
	// [ErrUnsupported] means that no atomic merge was started and the caller may
	// merge the planned changes individually.
	Merge(context.Context, MergeRangeRequest) (MergeOperation, error)
}

// StackChange identifies one member and its desired immediate base.
type StackChange struct {
	// Change identifies the stack member.
	Change ChangeID // required

	// BaseChange identifies the immediate base change
	// when that change is also included in the request.
	// It may be nil for a tree root.
	BaseChange ChangeID

	// BaseBranch is the desired provider-facing base branch.
	BaseBranch string // required
}
