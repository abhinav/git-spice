package forge

import (
	"encoding"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/git"
)

// MergeChangeOptions specifies options for a merge operation.
type MergeChangeOptions struct {
	// Method selects the forge merge strategy.
	// If zero, the forge uses its repository default.
	Method MergeMethod

	// HeadHash, if non-empty, causes the merge to fail
	// if the change's current head commit doesn't match.
	// This prevents merging a change whose content
	// has changed since the caller last inspected it.
	//
	// Not all forges support this; unsupported forges
	// ignore the field.
	HeadHash git.Hash
}

// MergeMethod names a forge-level strategy for merging a change request.
type MergeMethod int

const (
	// MergeMethodDefault leaves the merge strategy up to the forge.
	MergeMethodDefault MergeMethod = iota

	// MergeMethodMerge requests a two-parent merge commit.
	MergeMethodMerge

	// MergeMethodSquash requests a single squashed commit.
	MergeMethodSquash

	// MergeMethodRebase requests a rebase before merging.
	MergeMethodRebase
)

var (
	_ encoding.TextMarshaler   = MergeMethod(0)
	_ encoding.TextUnmarshaler = (*MergeMethod)(nil)
)

// UnmarshalText decodes a merge method from text.
func (m *MergeMethod) UnmarshalText(bs []byte) error {
	switch strings.ToLower(string(bs)) {
	case "", "default":
		*m = MergeMethodDefault
	case "merge":
		*m = MergeMethodMerge
	case "squash":
		*m = MergeMethodSquash
	case "rebase":
		*m = MergeMethodRebase
	default:
		return fmt.Errorf(
			"invalid value %q: expected merge, squash, or rebase",
			string(bs),
		)
	}
	return nil
}

// MarshalText encodes a merge method to text.
func (m MergeMethod) MarshalText() ([]byte, error) {
	switch m {
	case MergeMethodDefault:
		return []byte("default"), nil
	case MergeMethodMerge:
		return []byte("merge"), nil
	case MergeMethodSquash:
		return []byte("squash"), nil
	case MergeMethodRebase:
		return []byte("rebase"), nil
	default:
		return nil, fmt.Errorf("unknown merge method: %d", m)
	}
}

// String returns the text form of the merge method.
func (m MergeMethod) String() string {
	if m == MergeMethodDefault {
		return "default"
	}
	bs, err := m.MarshalText()
	if err != nil {
		return fmt.Sprintf("MergeMethod(%d)", int(m))
	}
	return string(bs)
}
