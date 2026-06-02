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
	"time"

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

//go:generate mockgen -destination=forgetest/mocks.go -package forgetest -typed -write_package_comment=false . Forge,RepositoryID,Repository,WithInlineComments,WithThreadResolution,WithCommentEdit

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

	FindChangesByBranch(ctx context.Context, branch string, opts FindChangesOptions) ([]*FindChangeItem, error)
	FindChangeByID(ctx context.Context, id ChangeID) (*FindChangeItem, error)
	ChangeStatuses(ctx context.Context, ids []ChangeID) ([]ChangeStatus, error)

	// ChangeChecksState reports the aggregate CI/checks
	// state for the given change.
	//
	// If the forge has no CI/checks integration
	// or the change has no required checks,
	// implementations should return ChecksPassed.
	ChangeChecksState(ctx context.Context, id ChangeID) (ChecksState, error)
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

// ChecksState represents the aggregate CI/checks
// status for a change.
//
// TODO: Teach this type the names of individual statuses
// so merge failures can report exactly what's blocking the user.
type ChecksState int

const (
	// ChecksPending indicates checks are still running.
	ChecksPending ChecksState = iota + 1

	// ChecksPassed indicates all checks have passed.
	ChecksPassed

	// ChecksFailed indicates one or more checks failed.
	ChecksFailed
)

func (s ChecksState) String() string {
	switch s {
	case ChecksPending:
		return "pending"
	case ChecksPassed:
		return "passed"
	case ChecksFailed:
		return "failed"
	default:
		return fmt.Sprintf("ChecksState(%d)", int(s))
	}
}

// GoString returns a Go-syntax representation.
func (s ChecksState) GoString() string {
	switch s {
	case ChecksPending:
		return "ChecksPending"
	case ChecksPassed:
		return "ChecksPassed"
	case ChecksFailed:
		return "ChecksFailed"
	default:
		return fmt.Sprintf("ChecksState(%d)", int(s))
	}
}

// Inline comment types and optional interfaces

// CommentScope describes how a comment is anchored to a change.
type CommentScope int

const (
	// CommentScopeUnknown is the zero value; treated by JSON
	// output as [CommentScopeLine] for legacy callers that did
	// not set Scope.
	CommentScopeUnknown CommentScope = iota

	// CommentScopeLine indicates the comment is anchored
	// to a specific line (or range) in a file diff.
	CommentScopeLine

	// CommentScopeFile indicates the comment is anchored
	// to a file but not to a specific line.
	CommentScopeFile

	// CommentScopePR indicates the comment is at the
	// change-request level: not anchored to a file or line.
	CommentScopePR
)

// String returns the canonical lowercase representation
// used in JSON output ("pr"|"file"|"line").
func (s CommentScope) String() string {
	switch s {
	case CommentScopePR:
		return "pr"
	case CommentScopeFile:
		return "file"
	case CommentScopeLine, CommentScopeUnknown:
		return "line"
	default:
		return "line"
	}
}

// CommentRange describes the inclusive line range a
// multi-line comment spans.
type CommentRange struct {
	// Start is the first line of the range.
	Start int

	// End is the last line of the range (inclusive).
	End int
}

// InlineCommentRequest describes a new comment to post on a
// change. The "inline" name is historical: comments may be
// pr-, file-, or line-scoped depending on [Scope].
type InlineCommentRequest struct {
	// Scope distinguishes pr-level, file-level, and line-level
	// comments. Zero value is treated as [CommentScopeLine].
	Scope CommentScope

	// Path is the file path relative to the repository root.
	// Empty for [CommentScopePR].
	Path string

	// Line is the line number in the new version of the file.
	// Zero for [CommentScopePR] and [CommentScopeFile].
	Line int

	// Range is non-nil for multi-line comments. When nil, the
	// comment is anchored to a single [Line].
	Range *CommentRange

	// Body is the markdown body of the comment.
	Body string

	// Side indicates which side of the diff the comment
	// applies to. Use "LEFT" for the old version
	// or "RIGHT" (default) for the new version.
	Side string

	// ThreadID is set when replying to an existing thread.
	// The format is forge-specific.
	ThreadID string
}

// InlineComment is a comment managed by the change's inline-
// comments API. Despite the historical name, comments may be
// pr-, file-, or line-scoped depending on [Scope].
type InlineComment struct {
	// ID is the forge-specific comment identifier.
	ID ChangeCommentID

	// ThreadID is the forge-specific thread identifier.
	ThreadID string

	// Scope reports how the comment is anchored to the change.
	// Zero value is treated as [CommentScopeLine].
	Scope CommentScope

	// Path is the file path relative to the repository root.
	// Empty for [CommentScopePR].
	Path string

	// Line is the line number in the diff.
	// Zero for [CommentScopePR] and [CommentScopeFile].
	Line int

	// Range is non-nil for multi-line comments. When nil, the
	// comment is anchored to a single line ([Line]).
	Range *CommentRange

	// Side indicates which side of the diff the comment applies
	// to: "LEFT" for the old version, "RIGHT" for the new
	// version. Empty for non-line scopes.
	Side string

	// CommitSHA is the commit the comment was authored against.
	// Empty when the forge does not track per-commit comments.
	CommitSHA string

	// Body is the markdown body of the comment.
	Body string

	// Author is the username of the comment author.
	Author string

	// Resolved indicates the comment thread is resolved.
	Resolved bool

	// Outdated indicates the comment is on an outdated diff:
	// the anchored line is no longer present in the change's
	// current head diff. Surfaced to the extension as "stale".
	Outdated bool

	// CreatedAt is the time the comment was created.
	CreatedAt time.Time
}

// ReviewEvent specifies the type of review being submitted.
type ReviewEvent int

const (
	// ReviewComment submits a review with comments only.
	ReviewComment ReviewEvent = iota

	// ReviewApprove submits an approving review.
	ReviewApprove

	// ReviewRequestChanges submits a review
	// requesting changes.
	ReviewRequestChanges
)

// ReviewRequest is a batch of inline comments
// submitted together as a single review.
type ReviewRequest struct {
	// Body is the overall review body (optional).
	Body string

	// Comments are the inline comments in the review.
	Comments []InlineCommentRequest

	// Event is the review event type.
	Event ReviewEvent
}

// WithInlineComments is an optional interface
// for forges that support inline/diff comments
// and code reviews.
type WithInlineComments interface {
	Repository

	// ListInlineComments lists inline/review comments
	// on a change.
	ListInlineComments(
		ctx context.Context, id ChangeID,
	) ([]*InlineComment, error)

	// SubmitReview posts a batch of inline comments
	// as a single review.
	SubmitReview(
		ctx context.Context, id ChangeID, req ReviewRequest,
	) error

	// PostInlineComment posts a single inline comment
	// outside of a batch review.
	PostInlineComment(
		ctx context.Context, id ChangeID,
		req InlineCommentRequest,
	) (*InlineComment, error)
}

// WithThreadResolution is an optional interface
// for forges that support resolving comment threads.
type WithThreadResolution interface {
	Repository

	// ResolveThread marks a comment thread as resolved.
	ResolveThread(ctx context.Context, threadID string) error

	// UnresolveThread marks a comment thread as unresolved.
	UnresolveThread(ctx context.Context, threadID string) error
}

// WithCommentEdit is an optional interface
// for forges that support editing existing comments.
type WithCommentEdit interface {
	Repository

	// EditComment updates the body of an existing comment.
	EditComment(
		ctx context.Context, id ChangeCommentID, body string,
	) error
}
