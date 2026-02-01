// Package forge provides an abstraction layer between git-spice
// and the underlying forge (e.g. GitHub, GitLab, Bitbucket).
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"sync"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
)

// Registry is a collection of known code forges.
type Registry struct {
	m sync.Map
}

// All returns an iterator over items in the Forge
// in an unspecified order.
func (r *Registry) All() iter.Seq[Forge] {
	return func(yield func(Forge) bool) {
		r.m.Range(func(_, value any) bool {
			return yield(value.(Forge))
		})
	}
}

// Register registers a Forge with the Registry.
// The Forge may be unregistered by calling the returned function.
func (r *Registry) Register(f Forge) (unregister func()) {
	id := f.ID()
	r.m.Store(id, f)
	return func() {
		r.m.Delete(id)
	}
}

// Lookup searches for a registered Forge by ID.
// It returns false if a forge with that ID is not known.
func (r *Registry) Lookup(id string) (Forge, bool) {
	f, ok := r.m.Load(id)
	if !ok {
		return nil, false
	}
	return f.(Forge), true
}

// MatchRemoteURL attempts to match the given remote URL with a registered forge.
// Returns the matched forge, and information about the matched repository.
func MatchRemoteURL(r *Registry, remoteURL string) (forge Forge, rid RepositoryID, ok bool) {
	for f := range r.All() {
		rid, err := f.ParseRemoteURL(remoteURL)
		if err == nil {
			return f, rid, true
		}
	}
	return nil, nil, false
}

// ErrUnsupportedURL indicates that the given remote URL
// does not match any registered forge.
var ErrUnsupportedURL = errors.New("unsupported URL")

// ErrNotFound indicates that a requested resource does not exist.
var ErrNotFound = errors.New("not found")

//go:generate mockgen -destination=forgetest/mocks.go -package forgetest -typed . Forge,RepositoryID,Repository

// TODO:
// Forge should become a struct with multiple interfaces or funcctions
// that it depends on in the underlying implementation.

// Forge is a forge that hosts Git repositories.
type Forge interface {
	// ID reports a unique identifier for the forge, e.g. "github".
	ID() string // TODO: Rename to "slug" or "name" as that's more correct

	// CLIPlugin returns a Kong plugin for this Forge.
	//
	// This will be installed into the application to provide
	// additional Forge-specific flags or environment variable overrides.
	//
	// Return nil if the forge does not require any extra CLI flags.
	CLIPlugin() any

	// ParseRemoteURL extracts information about a Forge-hosted repository
	// from the given remote URL, and returns a [RepositoryID] identifying it.
	//
	// Returns ErrUnsupportedURL if the remote URL does not match
	// this forge.
	//
	// This operation should not make any network requests,
	//
	// For example, this would take "https://github.com/foo/bar.git"
	// and return a GitHub RepositoryID for the repository "foo/bar".
	ParseRemoteURL(remoteURL string) (RepositoryID, error)

	// OpenRepository opens the remote repository that the given ID points to.
	OpenRepository(ctx context.Context, tok AuthenticationToken, repo RepositoryID) (Repository, error)
	// TODO: For GitHub, to avoid looking up the GQLID for the repository
	// every time, we need a layer of metadata that Open can provide
	// that is persisted to the store alongside branch state,
	// and used in follow-up Open calls to avoid looking it up again.

	// ChangeTemplatePaths reports the case-insensitive paths at which
	// it's possible to define change templates in the repository.
	ChangeTemplatePaths() []string

	// MarshalChangeID serializes the given change ID into a valid JSON blob.
	MarshalChangeID(ChangeID) (json.RawMessage, error)

	// UnmarshalChangeID deserializes the given JSON blob into a change ID.
	UnmarshalChangeID(json.RawMessage) (ChangeID, error)

	// MarshalChangeMetadata serializes the given change metadata
	// into a valid JSON blob.
	MarshalChangeMetadata(ChangeMetadata) (json.RawMessage, error)

	// UnmarshalChangeMetadata deserializes the given JSON blob
	// into change metadata.
	UnmarshalChangeMetadata(json.RawMessage) (ChangeMetadata, error)

	// AuthenticationFlow runs the authentication flow for the forge.
	// This may prompt the user, perform network requests, etc.
	//
	// The implementation should return a secret that the Forge
	// can serialize and store for future use.
	AuthenticationFlow(ctx context.Context, view ui.View) (AuthenticationToken, error)

	// SaveAuthenticationToken saves the given authentication token
	// to the secret stash.
	SaveAuthenticationToken(secret.Stash, AuthenticationToken) error

	// LoadAuthenticationToken loads the authentication token
	// from the secret stash.
	LoadAuthenticationToken(secret.Stash) (AuthenticationToken, error)

	// ClearAuthenticationToken removes the authentication token
	// from the secret stash.
	ClearAuthenticationToken(secret.Stash) error
}

// AuthenticationToken is a secret that results from a successful login.
// It will be persisted in a safe place,
// and re-used for future authentication with the forge.
//
// Implementations must embed this interface.
type AuthenticationToken interface {
	secret() // marker method
}

// RepositoryID is a unique identifier for a repository hosted on a Forge.
//
// It is cheap to calculate from the remote URL of the repository,
// without performing any network requests.
type RepositoryID interface {
	// String reports a human-readable name for the repository,
	// e.g. "foo/bar" for GitHub.
	String() string

	// ChangeURL returns the web URL for the given change ID hosted on the forge
	// in this repository.
	ChangeURL(changeID ChangeID) string
}

// ErrUnsubmittedBase indicates that a change cannot be submitted
// because the base branch has not been pushed yet.
var ErrUnsubmittedBase = errors.New("base branch has not been submitted yet")

// Repository is a Git repository hosted on a forge.
type Repository interface {
	Forge() Forge

	// SubmitChange creates a new change request in the repository.
	//
	// Special errors:
	//
	//  - ErrUnsubmittedBase indicates that the change cannot be submitted
	//    because the base branch has not been pushed to the remote yet.
	SubmitChange(ctx context.Context, req SubmitChangeRequest) (SubmitChangeResult, error)

	EditChange(ctx context.Context, id ChangeID, opts EditChangeOptions) error
	FindChangesByBranch(ctx context.Context, branch string, opts FindChangesOptions) ([]*FindChangeItem, error)
	FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error)
	ChangesStates(ctx context.Context, ids []ChangeID) ([]ChangeState, error)

	// Post, update, and delete comments on changes.
	PostChangeComment(context.Context, ChangeID, string) (ChangeCommentID, error)
	UpdateChangeComment(context.Context, ChangeCommentID, string) error
	DeleteChangeComment(context.Context, ChangeCommentID) error

	// List comments on a CR, optionally filtered per the given options.
	ListChangeComments(context.Context, ChangeID, *ListChangeCommentsOptions) iter.Seq2[*ListChangeCommentItem, error]

	// NewChangeMetadata builds a ChangeMetadata for the given change ID.
	//
	// This may perform network requests to fetch additional information
	// if necessary.
	NewChangeMetadata(ctx context.Context, id ChangeID) (ChangeMetadata, error)

	// ListChangeTemplates returns templates defined in the repository
	// for new change proposals.
	//
	// Returns an empty list if no templates are found.
	ListChangeTemplates(context.Context) ([]*ChangeTemplate, error)
}

// ChangeID is a unique identifier for a change in a repository.
type ChangeID interface {
	String() string
}

// ChangeCommentID is a unique identifier for a comment on a change.
type ChangeCommentID interface {
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

	// Limit specifies the maximum number of changes to return.
	// Changes are sorted by most recently updated.
	// Defaults to 10.
	Limit int
}

// ListChangeCommentsOptions specifies options for filtering
// and limiting comments listed by ListChangeComments.
//
// Conditions specified here are combined with AND.
type ListChangeCommentsOptions struct {
	// BodyMatchesAll specifies zero or more regular expressions
	// that must all match the comment body.
	//
	// If empty, all comments are returned.
	BodyMatchesAll []*regexp.Regexp

	// CanUpdate specifies whether only comments that can be updated
	// by the current user should be returned.
	//
	// If false, all comments are returned.
	CanUpdate bool
}

// ListChangeCommentItem is a single result from listing comments on a change.
type ListChangeCommentItem struct {
	ID   ChangeCommentID
	Body string
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
