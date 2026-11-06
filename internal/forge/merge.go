package forge

import (
	"context"
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

// MergeRangeRequest supplies the expected commits for a planned range.
// The plan determines which changes to merge and their order.
type MergeRangeRequest struct {
	// Method selects one merge strategy for the complete path.
	// If zero, the forge uses its repository default.
	Method MergeMethod

	// Heads supplies one expected commit for each planned change.
	// Order does not matter.
	Heads []ChangeHead // required
}

// ChangeHead associates a change with its expected head commit.
type ChangeHead struct {
	// Change identifies the change.
	Change ChangeID // required

	// HeadHash is the exact expected head commit of Change.
	HeadHash git.Hash // required
}

// MergeOperation reports asynchronous acceptance of a merge range.
type MergeOperation interface {
	// Status performs one provider status probe.
	// Pending means the provider has not accepted the request yet.
	// Accepted means the request was accepted for direct execution or queueing;
	// callers must continue observing change states for final completion.
	Status(context.Context) (MergeOperationStatus, error)
}

// MergeOperationStatus describes asynchronous merge request acceptance.
// The zero value is invalid.
type MergeOperationStatus int

const (
	// MergeOperationPending means the provider is still processing the request.
	MergeOperationPending MergeOperationStatus = iota + 1

	// MergeOperationAccepted means the provider accepted the request.
	MergeOperationAccepted
)

// String returns the text form of the merge operation status.
func (s MergeOperationStatus) String() string {
	switch s {
	case MergeOperationPending:
		return "pending"
	case MergeOperationAccepted:
		return "accepted"
	default:
		return fmt.Sprintf("MergeOperationStatus(%d)", int(s))
	}
}

// GoString returns a Go-syntax representation of the merge operation status.
func (s MergeOperationStatus) GoString() string {
	switch s {
	case MergeOperationPending:
		return "MergeOperationPending"
	case MergeOperationAccepted:
		return "MergeOperationAccepted"
	default:
		return fmt.Sprintf("MergeOperationStatus(%d)", int(s))
	}
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
