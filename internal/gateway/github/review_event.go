package github

// ReviewEvent is the action taken when submitting a pending review.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewevent.
type ReviewEvent int

// ReviewEvent values accepted by GitHub.
const (
	ReviewEventUnknown ReviewEvent = iota
	ReviewEventApprove
	ReviewEventRequestChanges
)

var reviewEventText = [...]string{
	ReviewEventUnknown:        "",
	ReviewEventApprove:        "APPROVE",
	ReviewEventRequestChanges: "REQUEST_CHANGES",
}

var reviewEventByText = enumByText[ReviewEvent](reviewEventText[:])

// MarshalText returns GitHub's GraphQL representation.
func (e ReviewEvent) MarshalText() ([]byte, error) {
	return marshalEnum(e, reviewEventText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (e *ReviewEvent) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, e, reviewEventByText)
}
