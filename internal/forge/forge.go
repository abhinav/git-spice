// Package forge provides an abstraction layer between git-spice
// and the underlying forge (e.g. GitHub, GitLab, Bitbucket).
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"go.abhg.dev/gs/internal/git/giturl"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/ui"
)

//go:generate mockgen -destination=forgetest/mocks.go -package forgetest -typed . Forge,RepositoryID,Repository

// TODO:
// Forge should become a struct with multiple interfaces or funcctions
// that it depends on in the underlying implementation.

// Definition describes a forge implementation before it is bound
// to a particular repository remote.
type Definition interface {
	ChangeMetadataCodec

	// ID reports a unique identifier for the forge, e.g. "github".
	ID() string

	// BaseURL reports the configured forge web URL.
	//
	// Remote URL inference uses the host and optional port from this URL.
	BaseURL() string

	// CLIPlugin returns a Kong plugin for this forge.
	//
	// This will be installed into the application to provide
	// additional Forge-specific flags or environment variable overrides.
	//
	// Return nil if the forge does not require any extra CLI flags.
	CLIPlugin() any

	// New constructs a forge instance for the given remote URL.
	//
	// The remote URL must be non-nil.
	// Implementations may use it to derive instance configuration
	// that is not known when command-line options are registered.
	New(remoteURL *giturl.URL) (Forge, error)
}

// Forge is a forge that hosts Git repositories.
type Forge interface {
	ChangeMetadataCodec

	// ID reports a unique identifier for the forge, e.g. "github".
	ID() string // TODO: Rename to "slug" or "name" as that's more correct

	// BaseURL reports the configured forge web URL.
	//
	// Remote URL inference uses the host and optional port from this URL.
	// Providers may also use the same configured URL for user-facing links.
	BaseURL() string

	// ParseRepositoryPath extracts information about a Forge-hosted repository
	// from an already-extracted repository path,
	// and returns a [RepositoryID] identifying it.
	//
	// Returns ErrUnsupportedURL if the path does not identify
	// this forge.
	//
	// This operation should not make any network requests.
	//
	// For example, this would take "/foo/bar.git" and return
	// a GitHub RepositoryID for the repository "foo/bar".
	ParseRepositoryPath(string) (RepositoryID, error)

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

// ChangeMetadataCodec serializes persisted forge change metadata.
type ChangeMetadataCodec interface {
	// MarshalChangeMetadata serializes the given change metadata
	// into a valid JSON blob.
	MarshalChangeMetadata(ChangeMetadata) (json.RawMessage, error)

	// UnmarshalChangeMetadata deserializes the given JSON blob
	// into change metadata.
	UnmarshalChangeMetadata(json.RawMessage) (ChangeMetadata, error)
}

// WithDisplayName is an optional interface for values with UI display names.
// If not implemented, the value's ID is used as the display name.
type WithDisplayName interface {
	// DisplayName returns a human-friendly name for the forge,
	// e.g. "Bitbucket (Atlassian)" instead of just "bitbucket".
	DisplayName() string
}

// GetDisplayName returns the display name for a forge or forge definition.
// If the value implements WithDisplayName, it returns DisplayName().
// Otherwise, it returns the value's ID.
func GetDisplayName(v interface{ ID() string }) string {
	if fd, ok := v.(WithDisplayName); ok {
		return fd.DisplayName()
	}
	return v.ID()
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

// ErrUnknown indicates that a requested forge is not registered.
var ErrUnknown = errors.New("unknown forge")

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

	// MergeChange merges an open change into its base branch.
	MergeChange(ctx context.Context, id ChangeID, opts MergeChangeOptions) error

	// CommandEnvironment returns forge-specific environment variables
	// for commands that operate on the given change.
	//
	// Keys must use the GIT_SPICE_ prefix.
	// Callers own common git-spice variables
	// and may ignore forge values that collide with those keys.
	CommandEnvironment(ctx context.Context, id ChangeID) (map[string]string, error)

	// ChangeMergeability reports whether the forge currently considers
	// the change mergeable.
	//
	// This reports the forge's merge decision,
	// not the detailed status of individual CI/checks signals.
	// Use ChangeChecks for check display and required-check inspection.
	ChangeMergeability(ctx context.Context, id ChangeID) (ChangeMergeability, error)

	FindChangesByBranch(ctx context.Context, branch string, opts FindChangesOptions) ([]*FindChangeItem, error)
	FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error)
	ChangeStatuses(ctx context.Context, ids []ChangeID) ([]ChangeStatus, error)

	// ChangeChecks reports CI/checks for the given change.
	//
	// If the forge has no CI/checks integration
	// or the change has no required checks,
	// implementations should return an empty slice.
	ChangeChecks(ctx context.Context, id ChangeID) ([]ChangeCheck, error)
	CommentCountsByChange(ctx context.Context, ids []ChangeID) ([]*CommentCounts, error)

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

// WithChangeURL is an optional interface that repositories can implement
// to provide URLs for changes.
// This is used to generate clickable links in navigation comments
// for forges that don't auto-link change references.
type WithChangeURL interface {
	Repository

	// ChangeURL returns the web URL for viewing the given change.
	ChangeURL(id ChangeID) string
}

// WithNavigationReference is an optional interface that repositories can
// implement to customize how a change is referenced inside stack
// navigation (comments or descriptions).
//
// Forges like GitLab support reference expansion (e.g. "!123+") that
// renders the change title inline when the markdown is rendered.
// Repositories that implement this interface take precedence over
// [WithChangeURL] for navigation rendering.
type WithNavigationReference interface {
	Repository

	// NavigationReference returns the markdown snippet used to reference
	// the given change ID in stack navigation content.
	NavigationReference(id ChangeID) string
}
