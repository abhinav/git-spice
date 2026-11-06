package github

// ReviewThreadSubjectType identifies whether a review thread targets a file
// or a line in its diff.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewthreadsubjecttype.
type ReviewThreadSubjectType int

// ReviewThreadSubjectType values reported by GitHub.
const (
	ReviewThreadSubjectTypeUnknown ReviewThreadSubjectType = iota
	ReviewThreadSubjectTypeFile
	ReviewThreadSubjectTypeLine
)

var reviewThreadSubjectTypeText = [...]string{
	ReviewThreadSubjectTypeUnknown: "",
	ReviewThreadSubjectTypeFile:    "FILE",
	ReviewThreadSubjectTypeLine:    "LINE",
}

var reviewThreadSubjectTypeByText = enumByText[ReviewThreadSubjectType](reviewThreadSubjectTypeText[:])

// MarshalText returns GitHub's GraphQL representation.
func (s ReviewThreadSubjectType) MarshalText() ([]byte, error) {
	return marshalEnum(s, reviewThreadSubjectTypeText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (s *ReviewThreadSubjectType) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, s, reviewThreadSubjectTypeByText)
}
