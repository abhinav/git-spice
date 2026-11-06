package github

// MergeableState is GitHub's conflict calculation for a pull request.
// See https://docs.github.com/en/graphql/reference/pulls#mergeablestate.
type MergeableState int

// MergeableState values reported by GitHub.
const (
	MergeableStateUnknown MergeableState = iota
	MergeableStateMergeable
	MergeableStateConflicting
)

var mergeableStateText = [...]string{
	MergeableStateUnknown:     "UNKNOWN",
	MergeableStateMergeable:   "MERGEABLE",
	MergeableStateConflicting: "CONFLICTING",
}

var mergeableStateByText = enumByText[MergeableState](mergeableStateText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s MergeableState) MarshalText() ([]byte, error) {
	return marshalEnum(s, mergeableStateText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *MergeableState) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, mergeableStateByText)
}

// MergeStateStatus is GitHub's combined merge-requirements state.
// See https://docs.github.com/en/graphql/reference/pulls#mergestatestatus.
type MergeStateStatus int

// MergeStateStatus values reported by GitHub.
const (
	MergeStateStatusUnknown MergeStateStatus = iota
	MergeStateStatusBehind
	MergeStateStatusBlocked
	MergeStateStatusClean
	MergeStateStatusDirty
	MergeStateStatusDraft
	MergeStateStatusHasHooks
	MergeStateStatusUnstable
)

var mergeStateStatusText = [...]string{
	MergeStateStatusUnknown:  "UNKNOWN",
	MergeStateStatusBehind:   "BEHIND",
	MergeStateStatusBlocked:  "BLOCKED",
	MergeStateStatusClean:    "CLEAN",
	MergeStateStatusDirty:    "DIRTY",
	MergeStateStatusDraft:    "DRAFT",
	MergeStateStatusHasHooks: "HAS_HOOKS",
	MergeStateStatusUnstable: "UNSTABLE",
}

var mergeStateStatusByText = enumByText[MergeStateStatus](mergeStateStatusText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s MergeStateStatus) MarshalText() ([]byte, error) {
	return marshalEnum(s, mergeStateStatusText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *MergeStateStatus) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, mergeStateStatusByText)
}
