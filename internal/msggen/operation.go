package msggen

// OperationKind identifies the command invoking message generation.
type OperationKind int

// Supported operations map command names to their script-facing values.
const (
	OperationUnspecified OperationKind = iota
	CommitCreate
	CommitAmend
	BranchCreate
	BranchSquash
	BranchSubmit
	StackSubmit
	UpstackSubmit
	DownstackSubmit
)

// String returns the full command name with its words joined by a hyphen.
func (op OperationKind) String() string {
	switch op {
	case CommitCreate:
		return "commit-create"
	case CommitAmend:
		return "commit-amend"
	case BranchCreate:
		return "branch-create"
	case BranchSquash:
		return "branch-squash"
	case BranchSubmit:
		return "branch-submit"
	case StackSubmit:
		return "stack-submit"
	case UpstackSubmit:
		return "upstack-submit"
	case DownstackSubmit:
		return "downstack-submit"
	default:
		return "unspecified"
	}
}
