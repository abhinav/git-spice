package github

// ReviewState is the state of a GitHub pull request review.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewstate.
type ReviewState int

// ReviewState values reported by GitHub.
const (
	ReviewStateUnknown ReviewState = iota
	ReviewStateApproved
	ReviewStateChangesRequested
	ReviewStateCommented
	ReviewStateDismissed
	ReviewStatePending
)

var reviewStateText = [...]string{
	ReviewStateUnknown:          "",
	ReviewStateApproved:         "APPROVED",
	ReviewStateChangesRequested: "CHANGES_REQUESTED",
	ReviewStateCommented:        "COMMENTED",
	ReviewStateDismissed:        "DISMISSED",
	ReviewStatePending:          "PENDING",
}

var reviewStateByText = enumByText[ReviewState](reviewStateText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s ReviewState) MarshalText() ([]byte, error) {
	return marshalEnum(s, reviewStateText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *ReviewState) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, reviewStateByText)
}
