package forge

import "context"

// StackRepository provides optional support for forge-native stacks.
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
