package submit

import (
	"encoding"
	"fmt"
	"strings"
)

// SkipRestackCheck specifies when the restack check
// should be skipped during submit.
type SkipRestackCheck int

const (
	// SkipRestackCheckNever never skips the restack check.
	// This is the default behavior.
	SkipRestackCheckNever SkipRestackCheck = iota

	// SkipRestackCheckTrunk skips the restack check
	// for branches based directly on trunk.
	SkipRestackCheckTrunk

	// SkipRestackCheckAlways skips the restack check
	// for all branches.
	SkipRestackCheckAlways
)

var _ encoding.TextUnmarshaler = (*SkipRestackCheck)(nil)

// String returns the string representation
// of the SkipRestackCheck value.
func (s SkipRestackCheck) String() string {
	switch s {
	case SkipRestackCheckNever:
		return "never"
	case SkipRestackCheckTrunk:
		return "trunk"
	case SkipRestackCheckAlways:
		return "always"
	default:
		return "unknown"
	}
}

// UnmarshalText decodes SkipRestackCheck from text.
// It accepts "never"/"false", "trunk", and "always"/"true".
// Matching is case-insensitive.
func (s *SkipRestackCheck) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "never", "false":
		*s = SkipRestackCheckNever
	case "trunk":
		*s = SkipRestackCheckTrunk
	case "always", "true":
		*s = SkipRestackCheckAlways
	default:
		return fmt.Errorf(
			"invalid value %q:"+
				" expected never, trunk, or always",
			bs,
		)
	}
	return nil
}

// shouldSkipRestackCheck reports whether the restack check
// should be skipped for a branch with the given base.
func shouldSkipRestackCheck(
	mode SkipRestackCheck,
	base, trunk string,
) bool {
	switch mode {
	case SkipRestackCheckTrunk:
		return base == trunk
	case SkipRestackCheckAlways:
		return true
	default:
		return false
	}
}
