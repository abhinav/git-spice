package github

// DiffSide identifies one side of a GitHub pull request diff.
// See https://docs.github.com/en/graphql/reference/pulls#diffside.
type DiffSide int

// DiffSide values reported and accepted by GitHub.
const (
	DiffSideUnknown DiffSide = iota
	DiffSideLeft
	DiffSideRight
)

var diffSideText = [...]string{
	DiffSideUnknown: "",
	DiffSideLeft:    "LEFT",
	DiffSideRight:   "RIGHT",
}

var diffSideByText = enumByText[DiffSide](diffSideText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s DiffSide) MarshalText() ([]byte, error) {
	return marshalEnum(s, diffSideText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *DiffSide) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, diffSideByText)
}
