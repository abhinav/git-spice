// Package forge provides an abstraction layer between git-spice
// and the underlying forge (e.g. GitHub, GitLab, Bitbucket).
package forge

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/git/giturl"
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

// FromRemoteURL attempts to match the given remote URL with a registered forge.
// It returns the matched forge and information about the matched repository.
func FromRemoteURL(r *Registry, remoteURL *giturl.URL) (forge Forge, rid RepositoryID, ok bool) {
	for f := range r.All() {
		baseURL, err := url.Parse(f.BaseURL())
		if err != nil {
			continue
		}

		baseHost := baseURL.Hostname()
		remoteHost := remoteURL.Hostname
		// Some forges advertise a base URL such as "https://github.com",
		// while Git remotes use a related SSH hostname like "ssh.github.com".
		// Accept subdomains so these documented SSH hosts still infer
		// the same forge.
		hostMatches := remoteHost == baseHost ||
			strings.HasSuffix(remoteHost, "."+baseHost)
		if !hostMatches {
			continue
		}

		// A base URL without an explicit port describes the forge host,
		// not one transport endpoint.
		// In that case, allow the remote to specify its SSH port.
		basePort := baseURL.Port()
		if basePort != "" && remoteURL.Port != basePort {
			continue
		}

		rid, err := f.ParseRepositoryPath(remoteURL.Path)
		if err == nil {
			return f, rid, true
		}
	}
	return nil, nil, false
}

// SplitRepositoryPath extracts owner and repository name from a URL path.
//
// It strips leading/trailing slashes and the ".git" suffix,
// then splits on the first slash to get owner/repository components.
// For example,
// "/owner/repo.git" returns "owner" and "repo";
// "/workspace/repo/" returns "workspace" and "repo".
func SplitRepositoryPath(path string) (owner, repo string, ok bool) {
	s := strings.TrimPrefix(path, "/")
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")

	owner, repo, ok = strings.Cut(s, "/")
	return owner, repo, ok
}

// ErrUnsupportedURL indicates that the given remote URL
// does not match any registered forge.
var ErrUnsupportedURL = errors.New("unsupported URL")

// ErrNotFound indicates that a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrCommentCannotUpdate indicates that an existing comment cannot be updated.
// This typically occurs when local state is missing required information
// (e.g., PR ID for Bitbucket comments).
// Callers should handle this by posting a new comment instead.
var ErrCommentCannotUpdate = errors.New("comment cannot be updated")

// CommentFormat specifies formatting preferences for navigation comments.
type CommentFormat struct {
	// Footer is appended at the end of the navigation comment.
	// Defaults to HTML <sub> tag if empty.
	Footer string

	// Marker is an invisible marker used to identify navigation comments.
	// Defaults to HTML comment if empty.
	Marker string
}

// WithCommentFormat is an optional interface that forges can implement
// to customize navigation comment formatting.
// This is useful for forges like Bitbucket that don't support HTML in comments.
type WithCommentFormat interface {
	Forge

	// CommentFormat returns custom formatting for navigation comments.
	CommentFormat() CommentFormat
}

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

// WithDisplayName is an optional interface that forges can implement
// to provide a human-friendly display name for the UI.
// If not implemented, the forge's ID is used as the display name.
type WithDisplayName interface {
	Forge

	// DisplayName returns a human-friendly name for the forge,
	// e.g. "Bitbucket (Atlassian)" instead of just "bitbucket".
	DisplayName() string
}

// GetDisplayName returns the display name for a forge.
// If the forge implements WithDisplayName, it returns DisplayName().
// Otherwise, it returns the forge's ID.
func GetDisplayName(f Forge) string {
	if fd, ok := f.(WithDisplayName); ok {
		return fd.DisplayName()
	}
	return f.ID()
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

	// PushRepository is the repository that owns the head branch.
	// If nil, only changes whose head branch is owned by the target
	// repository are returned.
	PushRepository RepositoryID

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

// CommentCounts represents comment/thread resolution counts on a change.
type CommentCounts struct {
	// Total is the total number of resolvable comments or threads.
	Total int

	// Resolved is the number of resolved comments or threads.
	Resolved int

	// Unresolved is the number of unresolved comments or threads.
	Unresolved int
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
