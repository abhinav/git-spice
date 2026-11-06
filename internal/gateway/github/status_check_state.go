package github

// StatusState is GitHub's state for a classic commit status.
// See https://docs.github.com/en/graphql/reference/commits#statusstate.
type StatusState int

// StatusState values reported by GitHub.
const (
	StatusStateUnknown StatusState = iota
	StatusStateExpected
	StatusStateError
	StatusStateFailure
	StatusStatePending
	StatusStateSuccess
)

var statusStateText = [...]string{
	StatusStateUnknown:  "",
	StatusStateExpected: "EXPECTED",
	StatusStateError:    "ERROR",
	StatusStateFailure:  "FAILURE",
	StatusStatePending:  "PENDING",
	StatusStateSuccess:  "SUCCESS",
}

var statusStateByText = enumByText[StatusState](statusStateText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s StatusState) MarshalText() ([]byte, error) {
	return marshalEnum(s, statusStateText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *StatusState) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, statusStateByText)
}

// CheckStatusState is GitHub's execution state for a check run.
// See https://docs.github.com/en/graphql/reference/checks#checkstatusstate.
type CheckStatusState int

// CheckStatusState values reported by GitHub.
const (
	CheckStatusStateUnknown CheckStatusState = iota
	CheckStatusStateCompleted
	CheckStatusStateInProgress
	CheckStatusStatePending
	CheckStatusStateQueued
	CheckStatusStateRequested
	CheckStatusStateWaiting
)

var checkStatusStateText = [...]string{
	CheckStatusStateUnknown:    "",
	CheckStatusStateCompleted:  "COMPLETED",
	CheckStatusStateInProgress: "IN_PROGRESS",
	CheckStatusStatePending:    "PENDING",
	CheckStatusStateQueued:     "QUEUED",
	CheckStatusStateRequested:  "REQUESTED",
	CheckStatusStateWaiting:    "WAITING",
}

var checkStatusStateByText = enumByText[CheckStatusState](checkStatusStateText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s CheckStatusState) MarshalText() ([]byte, error) {
	return marshalEnum(s, checkStatusStateText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *CheckStatusState) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, checkStatusStateByText)
}

// CheckConclusionState is GitHub's terminal result for a check run.
// See https://docs.github.com/en/graphql/reference/checks#checkconclusionstate.
type CheckConclusionState int

// CheckConclusionState values reported by GitHub.
const (
	CheckConclusionStateUnknown CheckConclusionState = iota
	CheckConclusionStateActionRequired
	CheckConclusionStateCancelled
	CheckConclusionStateFailure
	CheckConclusionStateNeutral
	CheckConclusionStateSkipped
	CheckConclusionStateStale
	CheckConclusionStateStartupFailure
	CheckConclusionStateSuccess
	CheckConclusionStateTimedOut
)

var checkConclusionStateText = [...]string{
	CheckConclusionStateUnknown:        "",
	CheckConclusionStateActionRequired: "ACTION_REQUIRED",
	CheckConclusionStateCancelled:      "CANCELLED",
	CheckConclusionStateFailure:        "FAILURE",
	CheckConclusionStateNeutral:        "NEUTRAL",
	CheckConclusionStateSkipped:        "SKIPPED",
	CheckConclusionStateStale:          "STALE",
	CheckConclusionStateStartupFailure: "STARTUP_FAILURE",
	CheckConclusionStateSuccess:        "SUCCESS",
	CheckConclusionStateTimedOut:       "TIMED_OUT",
}

var checkConclusionStateByText = enumByText[CheckConclusionState](checkConclusionStateText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s CheckConclusionState) MarshalText() ([]byte, error) {
	return marshalEnum(s, checkConclusionStateText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *CheckConclusionState) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, checkConclusionStateByText)
}
