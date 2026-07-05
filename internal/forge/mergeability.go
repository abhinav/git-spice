package forge

import "fmt"

// ChangeMergeability is a forge-independent summary of whether a change
// can be merged under the forge's current policy.
type ChangeMergeability struct {
	// State reports the forge's current mergeability decision.
	State ChangeMergeabilityState

	// Reason reports why the state is waiting or blocked.
	//
	// Ready, unknown, and unsupported states must use
	// ChangeMergeabilityReasonUnknown.
	// Waiting and blocked states should use the most specific reason the forge
	// exposes, or ChangeMergeabilityReasonUnknown if the forge exposes no
	// forge-neutral reason.
	Reason ChangeMergeabilityReason
}

// ChangeMergeabilityState describes the forge's current mergeability decision.
type ChangeMergeabilityState int

const (
	// ChangeMergeabilityUnknown indicates that the forge returned no usable
	// mergeability state for this request.
	//
	// This is not a waiting state.
	// Callers should not assume retrying will produce a more specific answer.
	ChangeMergeabilityUnknown ChangeMergeabilityState = iota

	// ChangeMergeabilityUnsupported indicates that the forge implementation
	// does not support mergeability.
	ChangeMergeabilityUnsupported

	// ChangeMergeabilityReady indicates that the forge currently allows
	// the change to be merged.
	ChangeMergeabilityReady

	// ChangeMergeabilityWaiting indicates that the forge has not reached
	// a final mergeability decision yet.
	ChangeMergeabilityWaiting

	// ChangeMergeabilityBlocked indicates that the forge currently rejects
	// merging the change.
	ChangeMergeabilityBlocked
)

func (s ChangeMergeabilityState) String() string {
	switch s {
	case ChangeMergeabilityUnknown:
		return "unknown"
	case ChangeMergeabilityUnsupported:
		return "unsupported"
	case ChangeMergeabilityReady:
		return "ready"
	case ChangeMergeabilityWaiting:
		return "waiting"
	case ChangeMergeabilityBlocked:
		return "blocked"
	default:
		return fmt.Sprintf("ChangeMergeabilityState(%d)", int(s))
	}
}

// GoString returns a Go-syntax representation of the mergeability state.
func (s ChangeMergeabilityState) GoString() string {
	switch s {
	case ChangeMergeabilityUnknown:
		return "ChangeMergeabilityUnknown"
	case ChangeMergeabilityUnsupported:
		return "ChangeMergeabilityUnsupported"
	case ChangeMergeabilityReady:
		return "ChangeMergeabilityReady"
	case ChangeMergeabilityWaiting:
		return "ChangeMergeabilityWaiting"
	case ChangeMergeabilityBlocked:
		return "ChangeMergeabilityBlocked"
	default:
		return fmt.Sprintf("ChangeMergeabilityState(%d)", int(s))
	}
}

// ChangeMergeabilityReason gives the primary forge-neutral reason why
// mergeability is waiting or blocked.
type ChangeMergeabilityReason int

const (
	// ChangeMergeabilityReasonUnknown indicates that no more specific reason
	// is available.
	ChangeMergeabilityReasonUnknown ChangeMergeabilityReason = iota

	// ChangeMergeabilityReasonChecks indicates that CI or status checks
	// are preventing a ready mergeability decision.
	ChangeMergeabilityReasonChecks

	// ChangeMergeabilityReasonReview indicates that review or approval policy
	// determines mergeability.
	ChangeMergeabilityReasonReview

	// ChangeMergeabilityReasonDraft indicates that the change is still a draft.
	ChangeMergeabilityReasonDraft

	// ChangeMergeabilityReasonConflicts indicates that merge conflicts
	// determine mergeability.
	ChangeMergeabilityReasonConflicts

	// ChangeMergeabilityReasonBehind indicates that the change must be updated
	// with its base branch before merging.
	ChangeMergeabilityReasonBehind

	// ChangeMergeabilityReasonDiscussions indicates that unresolved
	// discussions or comments determine mergeability.
	ChangeMergeabilityReasonDiscussions

	// ChangeMergeabilityReasonPolicy indicates that a forge or repository
	// policy determines mergeability.
	ChangeMergeabilityReasonPolicy
)

func (r ChangeMergeabilityReason) String() string {
	switch r {
	case ChangeMergeabilityReasonUnknown:
		return "unknown"
	case ChangeMergeabilityReasonChecks:
		return "checks"
	case ChangeMergeabilityReasonReview:
		return "review"
	case ChangeMergeabilityReasonDraft:
		return "draft"
	case ChangeMergeabilityReasonConflicts:
		return "conflicts"
	case ChangeMergeabilityReasonBehind:
		return "behind"
	case ChangeMergeabilityReasonDiscussions:
		return "discussions"
	case ChangeMergeabilityReasonPolicy:
		return "policy"
	default:
		return fmt.Sprintf("ChangeMergeabilityReason(%d)", int(r))
	}
}

// GoString returns a Go-syntax representation of the mergeability reason.
func (r ChangeMergeabilityReason) GoString() string {
	switch r {
	case ChangeMergeabilityReasonUnknown:
		return "ChangeMergeabilityReasonUnknown"
	case ChangeMergeabilityReasonChecks:
		return "ChangeMergeabilityReasonChecks"
	case ChangeMergeabilityReasonReview:
		return "ChangeMergeabilityReasonReview"
	case ChangeMergeabilityReasonDraft:
		return "ChangeMergeabilityReasonDraft"
	case ChangeMergeabilityReasonConflicts:
		return "ChangeMergeabilityReasonConflicts"
	case ChangeMergeabilityReasonBehind:
		return "ChangeMergeabilityReasonBehind"
	case ChangeMergeabilityReasonDiscussions:
		return "ChangeMergeabilityReasonDiscussions"
	case ChangeMergeabilityReasonPolicy:
		return "ChangeMergeabilityReasonPolicy"
	default:
		return fmt.Sprintf("ChangeMergeabilityReason(%d)", int(r))
	}
}
