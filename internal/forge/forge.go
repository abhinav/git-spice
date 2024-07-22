// Package forge provides an abstraction layer between git-spice
// and the underlying forge (e.g. GitHub, GitLab, Bitbucket).
package forge

// TODO: Rename this package to codeforge or something similar
// so we can use "forge" in variable names more easily.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/secret"
)

var _forgeRegistry sync.Map

// All is an iterator that yields all registered forges.
func All(yield func(Forge) bool) {
	_forgeRegistry.Range(func(_, value any) bool {
		return yield(value.(Forge))
	})
}

// IDs returns a sorted list of all registered forge IDs.
func IDs() []string {
	var names []string
	All(func(f Forge) bool {
		names = append(names, f.ID())
		return true
	})
	sort.Strings(names)
	return names
}

// Register registers a forge with the given ID.
// Returns a function to unregister the forge.
func Register(f Forge) (unregister func()) {
	id := f.ID()
	_forgeRegistry.Store(id, f)
	return func() {
		_forgeRegistry.Delete(id)
	}
}

// Lookup looks up a registered forge by its ID.
func Lookup(id string) (Forge, bool) {
	f, ok := _forgeRegistry.Load(id)
	if !ok {
		return nil, false
	}
	return f.(Forge), true
}

// MatchForgeURL attempts to match the given remote URL with a registered forge.
// Returns the matched forge and true if a match was found.
func MatchForgeURL(remoteURL string) (forge Forge, ok bool) {
	_forgeRegistry.Range(func(_, value any) (keepGoing bool) {
		f := value.(Forge)
		if f.MatchURL(remoteURL) {
			forge = f
			ok = true
			return false
		}
		return true
	})
	return forge, ok
}

// ErrUnsupportedURL indicates that the given remote URL
// does not match any registered forge.
var ErrUnsupportedURL = errors.New("unsupported URL")

// Forge is a forge that hosts Git repositories.
type Forge interface {
	// ID reports a unique identifier for the forge, e.g. "github".
	ID() string

	// CLIPlugin returns a Kong plugin for this Forge.
	// Return nil if the forge does not require any extra CLI flags.
	CLIPlugin() any
	// TODO: Perhaps some validation function for the flags?

	// MatchURL reports whether the given remote URL is hosted on the forge.
	MatchURL(remoteURL string) bool

	// OpenURL opens a repository hosted on the Forge
	// with the given remote URL.
	//
	// This will only be called if MatchURL reports true.
	OpenURL(ctx context.Context, tok AuthenticationToken, remoteURL string) (Repository, error)

	// ChangeTemplatePaths reports the case-insensitive paths at which
	// it's possible to define change templates in the repository.
	ChangeTemplatePaths() []string

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
	AuthenticationFlow(ctx context.Context) (AuthenticationToken, error)

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

// Repository is a Git repository hosted on a forge.
type Repository interface {
	Forge() Forge

	SubmitChange(ctx context.Context, req SubmitChangeRequest) (SubmitChangeResult, error)
	EditChange(ctx context.Context, id ChangeID, opts EditChangeOptions) error
	FindChangesByBranch(ctx context.Context, branch string, opts FindChangesOptions) ([]*FindChangeItem, error)
	FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error)
	ChangeIsMerged(ctx context.Context, id ChangeID) (bool, error)

	// Post and update comments on changes.
	PostChangeComment(context.Context, ChangeID, string) (ChangeCommentID, error)
	UpdateChangeComment(context.Context, ChangeCommentID, string) error

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

	// StackCommentID is a comment left on the Change
	// that contains a visualization of the stack.
	StackCommentID() ChangeCommentID

	// SetStackCommentID sets the ID of the stack comment
	// on the chnage metadata to persist it later.
	//
	// The ID may be nil to indicate that there is no stack comment.
	SetStackCommentID(ChangeCommentID)
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
}

// FindChangeItem is a single result from searching for changes in the
// repository.
type FindChangeItem struct {
	// ID is a unique identifier for the change.
	ID ChangeID

	// URL is the web URL at which the change can be viewed.
	URL string

	// State is the current state of the change.
	State ChangeState

	// Subject is the title of the change.
	Subject string

	// HeadHash is the hash of the commit at the top of the change.
	HeadHash git.Hash

	// BaseName is the name of the base branch
	// that this change is proposed against.
	BaseName string

	// Draft is true if the change is not yet ready to be reviewed.
	Draft bool
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
