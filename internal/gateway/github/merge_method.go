package github

// MergeMethod is GitHub's strategy for merging a pull request.
// See https://docs.github.com/en/graphql/reference/pulls#pullrequestmergemethod.
type MergeMethod int

// MergeMethod values accepted by GitHub.
const (
	MergeMethodUnknown MergeMethod = iota
	MergeMethodMerge
	MergeMethodSquash
	MergeMethodRebase
)

var mergeMethodText = [...]string{
	MergeMethodUnknown: "",
	MergeMethodMerge:   "MERGE",
	MergeMethodSquash:  "SQUASH",
	MergeMethodRebase:  "REBASE",
}

var mergeMethodByText = enumByText[MergeMethod](mergeMethodText[:])

// MarshalText returns GitHub's GraphQL representation.
func (m MergeMethod) MarshalText() ([]byte, error) {
	return marshalEnum(m, mergeMethodText[:])
}

// UnmarshalText decodes GitHub's GraphQL representation.
func (m *MergeMethod) UnmarshalText(text []byte) error {
	return unmarshalEnum(text, m, mergeMethodByText)
}
