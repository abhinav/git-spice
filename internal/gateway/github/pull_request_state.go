package github

// PullRequestState is GitHub's lifecycle state for a pull request.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequeststate.
type PullRequestState int

// PullRequestState values reported by GitHub.
const (
	PullRequestStateUnknown PullRequestState = iota
	PullRequestStateOpen
	PullRequestStateClosed
	PullRequestStateMerged
)

var pullRequestStateText = [...]string{
	PullRequestStateUnknown: "",
	PullRequestStateOpen:    "OPEN",
	PullRequestStateClosed:  "CLOSED",
	PullRequestStateMerged:  "MERGED",
}

var pullRequestStateByText = enumByText[PullRequestState](pullRequestStateText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s PullRequestState) MarshalText() ([]byte, error) {
	return marshalEnum(s, pullRequestStateText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *PullRequestState) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, pullRequestStateByText)
}
