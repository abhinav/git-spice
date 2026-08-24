package forge

import "context"

// StackRepository is an optional repository capability
// for repositories that support forge-native stacks.
type StackRepository interface {
	Repository

	// PlanStackUpdate prepares an update that will make provider state represent
	// the supplied change relationships.
	// Each entry identifies a change and, when present, its immediate base from
	// the same request. BaseBranch identifies the provider-facing branch even
	// when BaseChange is absent from the request.
	//
	// Planning is read-only. [ErrUnsupported] means that no provider state was
	// changed and the caller may apply its ordinary per-change base updates.
	// Other planning failures also leave provider state unchanged.
	//
	// Provider-specific limits on individual trees do not make the capability
	// unsupported. Implementations may warn and omit those trees from the
	// returned plan.
	PlanStackUpdate(context.Context, []StackChange) (StackUpdatePlan, error)
}

// StackUpdatePlan owns one prepared provider stack transition.
type StackUpdatePlan interface {
	// Execute applies the transition prepared by PlanStackUpdate.
	//
	// Execute may update independent trees before returning an error. It never
	// returns [ErrUnsupported]; callers must prepare a new plan before retrying
	// after any result.
	Execute(context.Context) error
}

// StackChange identifies one member and its desired immediate base.
type StackChange struct {
	// Change identifies the stack member.
	Change ChangeID // required

	// BaseChange identifies the immediate base change when that change is also
	// included in the request. It may be nil for a tree root.
	BaseChange ChangeID

	// BaseBranch is the desired provider-facing base branch.
	BaseBranch string // required
}
