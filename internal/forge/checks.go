package forge

import "fmt"

// ChangeCheck is a forge-independent status check
// reported for a change.
type ChangeCheck struct {
	// Name identifies the status check.
	Name string

	// State reports whether the status check is still running,
	// passed, or failed.
	State ChangeCheckState
}

// ChangeCheckState represents the state of one CI/checks signal
// reported for a change.
type ChangeCheckState int

const (
	// ChangeCheckPending indicates a check is still running.
	ChangeCheckPending ChangeCheckState = iota + 1

	// ChangeCheckPassed indicates a check has passed.
	ChangeCheckPassed

	// ChangeCheckFailed indicates a check has failed.
	ChangeCheckFailed
)

func (s ChangeCheckState) String() string {
	switch s {
	case ChangeCheckPending:
		return "pending"
	case ChangeCheckPassed:
		return "passed"
	case ChangeCheckFailed:
		return "failed"
	default:
		return fmt.Sprintf("ChangeCheckState(%d)", int(s))
	}
}

// GoString returns a Go-syntax representation.
func (s ChangeCheckState) GoString() string {
	switch s {
	case ChangeCheckPending:
		return "ChangeCheckPending"
	case ChangeCheckPassed:
		return "ChangeCheckPassed"
	case ChangeCheckFailed:
		return "ChangeCheckFailed"
	default:
		return fmt.Sprintf("ChangeCheckState(%d)", int(s))
	}
}
