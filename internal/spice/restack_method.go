package spice

import (
	"encoding"
	"fmt"
	"strings"
)

// RestackMethod specifies how a branch is replayed onto a new base
// during a restack.
type RestackMethod int

const (
	// RestackMethodRebase replays the branch's own commits
	// onto the new base with a rebase.
	// This is the default.
	RestackMethodRebase RestackMethod = iota

	// RestackMethodMerge merges the new base into the branch,
	// preserving the branch's existing commits
	// and recording a merge commit.
	RestackMethodMerge
)

var (
	_ encoding.TextUnmarshaler = (*RestackMethod)(nil)
	_ encoding.TextMarshaler   = RestackMethod(0)
)

// UnmarshalText parses a RestackMethod, defaulting to rebase when empty.
func (m *RestackMethod) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "rebase", "":
		*m = RestackMethodRebase
	case "merge":
		*m = RestackMethodMerge
	default:
		return fmt.Errorf(
			"invalid value %q: expected rebase or merge", bs)
	}
	return nil
}

// MarshalText implements [encoding.TextMarshaler].
func (m RestackMethod) MarshalText() ([]byte, error) {
	switch m {
	case RestackMethodRebase:
		return []byte("rebase"), nil
	case RestackMethodMerge:
		return []byte("merge"), nil
	default:
		return nil, fmt.Errorf("invalid value: %d", int(m))
	}
}

func (m RestackMethod) String() string {
	switch m {
	case RestackMethodRebase:
		return "rebase"
	case RestackMethodMerge:
		return "merge"
	default:
		return fmt.Sprintf("RestackMethod(%d)", int(m))
	}
}

// MergeAutoResolve specifies how a merge-based restack
// resolves textual conflicts automatically.
type MergeAutoResolve int

const (
	// MergeAutoResolveNone does not auto-resolve conflicts.
	// Conflicts interrupt the merge for manual resolution.
	// This is the default.
	MergeAutoResolveNone MergeAutoResolve = iota

	// MergeAutoResolveOurs resolves textual conflicts
	// in favor of the branch being restacked ("-X ours").
	MergeAutoResolveOurs

	// MergeAutoResolveTheirs resolves textual conflicts
	// in favor of the new base ("-X theirs").
	MergeAutoResolveTheirs
)

var (
	_ encoding.TextUnmarshaler = (*MergeAutoResolve)(nil)
	_ encoding.TextMarshaler   = MergeAutoResolve(0)
)

// UnmarshalText parses a MergeAutoResolve, defaulting to none when empty.
func (m *MergeAutoResolve) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "none", "":
		*m = MergeAutoResolveNone
	case "ours":
		*m = MergeAutoResolveOurs
	case "theirs":
		*m = MergeAutoResolveTheirs
	default:
		return fmt.Errorf(
			"invalid value %q: expected none, ours, or theirs", bs)
	}
	return nil
}

// MarshalText implements [encoding.TextMarshaler].
func (m MergeAutoResolve) MarshalText() ([]byte, error) {
	switch m {
	case MergeAutoResolveNone:
		return []byte("none"), nil
	case MergeAutoResolveOurs:
		return []byte("ours"), nil
	case MergeAutoResolveTheirs:
		return []byte("theirs"), nil
	default:
		return nil, fmt.Errorf("invalid value: %d", int(m))
	}
}

func (m MergeAutoResolve) String() string {
	switch m {
	case MergeAutoResolveNone:
		return "none"
	case MergeAutoResolveOurs:
		return "ours"
	case MergeAutoResolveTheirs:
		return "theirs"
	default:
		return fmt.Sprintf("MergeAutoResolve(%d)", int(m))
	}
}

// StrategyOption returns the [git.MergeRequest] strategy option
// corresponding to this auto-resolve mode:
// "" for none, "ours", or "theirs".
func (m MergeAutoResolve) StrategyOption() string {
	switch m {
	case MergeAutoResolveOurs:
		return "ours"
	case MergeAutoResolveTheirs:
		return "theirs"
	default:
		return ""
	}
}
