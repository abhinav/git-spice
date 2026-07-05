package forge

import (
	"fmt"

	"go.abhg.dev/gs/internal/git"
)

// ChangeID is a unique identifier for a change in a repository.
type ChangeID interface {
	String() string
}

// ChangeMetadata defines Forge-specific per-change metadata.
// This metadata is persisted to the state store alongside the branch state.
// It is used to track the relationship between a branch
// and its corresponding change in the forge.
//
// The implementation is per-forge, and should contain enough information
// for the forge to uniquely identify a change within a repository.
//
// The metadata must be JSON-serializable (as defined by methods on Forge).
type ChangeMetadata interface {
	ForgeID() string

	// ChangeID is a human-readable identifier for the change.
	// This is presented to the user in the UI.
	ChangeID() ChangeID

	// NavigationCommentID is a comment left on the Change
	// that contains a visualization of the stack.
	NavigationCommentID() ChangeCommentID

	// SetNavigationCommentID sets the ID of the navigation comment
	// on the chnage metadata to persist it later.
	//
	// The ID may be nil to indicate that there is no navigation comment.
	SetNavigationCommentID(ChangeCommentID)
}

// FindChangesOptions specifies filtering options
// for searching for changes.
type FindChangesOptions struct {
	State ChangeState // 0 = all

	// PushRepository is the repository that owns the head branch.
	// If nil, only changes whose head branch is owned by the target
	// repository are returned.
	PushRepository RepositoryID

	// Limit specifies the maximum number of changes to return.
	// Changes are sorted by most recently updated.
	// Defaults to 10.
	Limit int
}

// SubmitChangeRequest is a request to submit a new change in a repository.
// The change must have already been pushed to the remote.
type SubmitChangeRequest struct {
	// Subject is the title of the change.
	Subject string // required

	// Body is the description of the change.
	Body string

	// Base is the name of the base branch
	// that this change is proposed against.
	Base string // required

	// Head is the name of the branch containing the change.
	//
	// This must have already been pushed to the remote.
	Head string // required

	// PushRepository is the repository that owns the head branch.
	// If nil, the target repository owns the head branch.
	PushRepository RepositoryID

	// Draft specifies whether the change should be marked as a draft.
	Draft bool

	// Labels are optional labels to apply to the change.
	Labels []string

	// Reviewers are optional reviewers to request reviews from.
	Reviewers []string

	// Assignees are optional users to assign to the change.
	Assignees []string
}

// SubmitChangeResult is the result of creating a new change in a repository.
type SubmitChangeResult struct {
	ID  ChangeID
	URL string
}

// EditChangeOptions specifies options for an operation to edit
// an existing change.
type EditChangeOptions struct {
	// Base specifies the name of the base branch.
	//
	// If unset, the base branch is not changed.
	Base string

	// Draft specifies whether the change should be marked as a draft.
	// If unset, the draft status is not changed.
	Draft *bool

	// AddLabels are the labels to apply to the change.
	// Existing labels associated with the change will not be modified.
	AddLabels []string

	// AddReviewers are new reviewers to request reviews from.
	// Existing reviewers associated with the change will not be modified.
	AddReviewers []string

	// AddAssignees are new users to assign to the change.
	// Existing assignees associated with the change will not be modified.
	AddAssignees []string
}

// FindChangeItem is a single result from searching for changes in the
// repository.
type FindChangeItem struct {
	// ID is a unique identifier for the change.
	ID ChangeID // required

	// URL is the web URL at which the change can be viewed.
	URL string // required

	// State is the current state of the change.
	State ChangeState // required

	// Subject is the title of the change.
	Subject string // required

	// HeadHash is the hash of the commit at the top of the change.
	HeadHash git.Hash // required

	// BaseName is the name of the base branch
	// that this change is proposed against.
	BaseName string // required

	// Draft is true if the change is not yet ready to be reviewed.
	Draft bool // required

	// Labels are the labels currently applied to the change.
	Labels []string

	// Reviewers are the usernames of users
	// who have been requested to review the change.
	Reviewers []string

	// Assignees are the usernames of users
	// who are assigned to the change.
	Assignees []string
}

// ChangeStatus is a compact status summary for a change.
type ChangeStatus struct {
	// State is the current state of the change.
	State ChangeState

	// HeadHash is the hash of the commit at the top of the change.
	HeadHash git.Hash
}

// ChangeTemplate is a template for a new change proposal.
type ChangeTemplate struct {
	// Filename is the name of the template file.
	//
	// This is NOT a path.
	Filename string

	// Body is the content of the template file.
	Body string
}

// ChangeState is the current state of a change.
type ChangeState int

const (
	// ChangeOpen specifies that a change is open.
	ChangeOpen ChangeState = iota + 1

	// ChangeMerged specifies that a change has been merged.
	ChangeMerged

	// ChangeClosed specifies that a change has been closed.
	ChangeClosed
)

func (s ChangeState) String() string {
	b, err := s.MarshalText()
	if err != nil {
		return "unknown"
	}
	return string(b)
}

// GoString returns a Go-syntax representation of the change state.
func (s ChangeState) GoString() string {
	switch s {
	case ChangeOpen:
		return "ChangeOpen"
	case ChangeMerged:
		return "ChangeMerged"
	case ChangeClosed:
		return "ChangeClosed"
	default:
		return fmt.Sprintf("ChangeState(%d)", int(s))
	}
}

// MarshalText serialize the change state to text.
// This implements encoding.TextMarshaler.
func (s ChangeState) MarshalText() ([]byte, error) {
	switch s {
	case ChangeOpen:
		return []byte("open"), nil
	case ChangeMerged:
		return []byte("merged"), nil
	case ChangeClosed:
		return []byte("closed"), nil
	default:
		return nil, fmt.Errorf("unknown change state: %d", s)
	}
}

// UnmarshalText parses the change state from text.
// This implements encoding.TextUnmarshaler.
func (s *ChangeState) UnmarshalText(b []byte) error {
	switch string(b) {
	case "open":
		*s = ChangeOpen
	case "merged":
		*s = ChangeMerged
	case "closed":
		*s = ChangeClosed
	default:
		return fmt.Errorf("unknown change state: %q", b)
	}
	return nil
}
