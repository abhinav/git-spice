// Package bitbucket defines the [Gateway] interface
// that abstracts the REST API differences
// between Bitbucket Cloud and Bitbucket Data Center,
// along with the product-neutral data types
// and error sentinels shared by its implementations.
package bitbucket

import (
	"context"
	"errors"
	"iter"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

// ErrUnsupported indicates that an optional gateway capability
// is not available on the Bitbucket product backing the gateway.
var ErrUnsupported = errors.New("not supported by this Bitbucket product")

// ErrMergeBlocked is returned when a pre-merge check blocks the merge.
var ErrMergeBlocked = errors.New("pull request cannot be merged")

// PullRequest is a product-neutral view of a Bitbucket pull request.
type PullRequest struct {
	// Number is the pull request ID.
	Number int64

	// URL is the web URL at which the pull request can be viewed.
	URL string

	// State is the state of the pull request,
	// already normalized from the product-specific representation.
	State forge.ChangeState

	// Subject is the title of the pull request.
	Subject string

	// BaseName is the name of the branch
	// that the pull request is proposed against.
	BaseName string

	// HeadHash is the best available commit for the pull request head.
	// Bitbucket Cloud falls back to the merge commit
	// when it does not report the source commit on merged pull requests.
	HeadHash git.Hash

	// Draft reports whether the pull request is marked as a draft.
	Draft bool

	// Reviewers are the usernames of users
	// who have been requested to review the pull request.
	Reviewers []string
}

// CreateChangeRequest is a request to create a new pull request.
type CreateChangeRequest struct {
	// Subject is the title of the pull request.
	Subject string

	// Body is the description of the pull request.
	Body string

	// Base is the name of the branch
	// that the pull request is proposed against.
	Base string

	// Head is the name of the branch containing the changes.
	Head string

	// PushRepository is the repository that owns the head branch.
	// If nil, the target repository owns the head branch.
	PushRepository forge.RepositoryID

	// Draft specifies whether the pull request
	// should be created as a draft.
	Draft bool

	// Reviewers are usernames of users to request reviews from.
	Reviewers []string
}

// ChangeUpdate specifies modifications to an existing pull request.
// Zero-valued fields are left unchanged.
type ChangeUpdate struct {
	// Base is the new base branch name.
	// If empty, the base branch is not changed.
	Base string

	// AddReviewers are usernames of users
	// to additionally request reviews from.
	// Existing reviewers are not modified.
	AddReviewers []string
}

// ChangeComment is a product-neutral view of a pull request comment.
type ChangeComment struct {
	// ID is the comment ID.
	ID int64

	// PRID is the ID of the pull request that the comment belongs to.
	PRID int64

	// Version is the comment's optimistic-locking version,
	// which Bitbucket Data Center requires on update and delete.
	//
	// It is always zero for Bitbucket Cloud comments.
	Version int

	// Body is the raw text of the comment.
	Body string
}

// ResolvableComment is a review comment
// that participates in comment resolution counts.
type ResolvableComment struct {
	// ID is the comment ID.
	ID int64

	// Body is the raw text of the comment.
	Body string

	// Resolved reports whether the comment has been resolved.
	Resolved bool

	// Pending reports whether the comment belongs to a review
	// that has not been published yet.
	//
	// Only Bitbucket Data Center reports pending comments;
	// it is always false on Bitbucket Cloud.
	Pending bool
}

// ReviewerState is the latest effective review state of a pull request
// participant.
type ReviewerState struct {
	// Reviewer is the participant's Bitbucket username.
	Reviewer string

	// Disposition is the participant's current review outcome.
	Disposition forge.ReviewDisposition

	// CommitHash is the revision reviewed by the participant.
	CommitHash git.Hash

	// SubmittedAt is when the participant established this state.
	SubmittedAt time.Time
}

// ReviewThread is a comment-backed discussion attached to a pull request diff.
// RootCommentID identifies the top-level inline comment that owns the replies.
type ReviewThread struct {
	// RootCommentID is the top-level anchored comment that owns the thread.
	RootCommentID int64

	// Path is the repository-relative file path attached to the root.
	Path string

	// Range is the inclusive range represented by the product anchor.
	// A zero range identifies the whole file.
	Range forge.ReviewThreadRange

	// Side is the preimage or postimage file selected by the anchor.
	// It is ignored when Range is zero.
	Side forge.ReviewThreadSide

	// Resolved is meaningful only when ReviewCapabilities.ThreadResolution is
	// true for the gateway.
	Resolved bool

	// Comments contains the root followed by its replies in product order.
	Comments []ReviewComment
}

// ReviewComment is one comment in a comment-backed review thread.
type ReviewComment struct {
	// ID is the product comment ID.
	ID int64

	// Version is the optimistic-locking version used by Data Center edits.
	// It is zero for Bitbucket Cloud.
	Version int

	// Body is the raw comment text.
	Body string

	// Author is the comment author's Bitbucket username.
	Author string

	// CreatedAt is the product-reported creation time.
	CreatedAt time.Time
}

// CreateReviewCommentRequest describes an inline comment or thread reply.
// ParentID is zero for a new inline comment. When it is non-zero, the location
// fields are ignored and the comment replies to that root comment.
type CreateReviewCommentRequest struct {
	// ParentID is the root comment ID for a reply, or zero for a new thread.
	ParentID int64

	// Path, Range, and Side locate a new thread and are ignored for replies.
	// A zero Range identifies the whole file and ignores Side.
	Path  string
	Range forge.ReviewThreadRange
	Side  forge.ReviewThreadSide

	// Body is the comment text.
	Body string

	// ReviewContext and ReviewAnchor carry Data Center's native draft anchor
	// inputs. Cloud ignores them.
	ReviewContext ReviewContext
	ReviewAnchor  ReviewAnchor
}

// ReviewCapabilities reports the review features available from a gateway.
// Bitbucket Cloud reports static capabilities. Bitbucket Data Center derives
// them from the running product version.
type ReviewCapabilities struct {
	// Supported reports whether the product can satisfy ReviewRepository.
	Supported bool

	// NativeDrafts reports whether review contents remain pending until
	// PublishReview.
	NativeDrafts bool

	// FileLevel reports whether root comments can be attached to a whole file.
	FileLevel bool

	// Multiline reports whether root anchors can span an inclusive range.
	Multiline bool

	// ThreadResolution reports whether the product exposes resolution state and
	// permits ResolveReviewThread and UnresolveReviewThread.
	ThreadResolution bool
}

// ReviewContext identifies the Data Center pull request revision on which a
// pending review is built. Cloud review submissions do not use it.
type ReviewContext struct {
	// BaseHash and HeadHash are the exact sinceId/untilId revision pair used by
	// Data Center diff lookup and comment anchors.
	BaseHash git.Hash
	HeadHash git.Hash

	// Version is the Data Center pull request optimistic-locking version used
	// when publishing the native review.
	Version int
}

// ReviewAnchor carries Data Center's diff-segment classifications for the
// inclusive review range. Cloud review submissions do not use it.
type ReviewAnchor struct {
	// StartLineType and EndLineType are Data Center diff segment values:
	// ADDED, REMOVED, or CONTEXT.
	StartLineType string
	EndLineType   string
}

// ReviewGateway is implemented by Bitbucket products that expose review
// conversations through pull request comments.
type ReviewGateway interface {
	Gateway

	ReviewCapabilities(context.Context) (ReviewCapabilities, error)
	ListReviewerStates(context.Context, int64) iter.Seq2[*ReviewerState, error]
	ListReviewThreads(context.Context, int64) iter.Seq2[*ReviewThread, error]
	CreateReviewComment(
		context.Context,
		int64,
		CreateReviewCommentRequest,
	) (*ReviewComment, error)
	ResolveReviewThread(context.Context, int64, int64) error
	UnresolveReviewThread(context.Context, int64, int64) error
}

// EmulatedReviewGateway applies dispositions outside a pending review workflow.
// Bitbucket Cloud implements this because its review resources publish
// immediately rather than through one native completion endpoint.
type EmulatedReviewGateway interface {
	ReviewGateway

	SetReviewDisposition(context.Context, int64, forge.ReviewDisposition) error
}

// PendingReviewGateway publishes comments, summary, and disposition as one
// native review. Bitbucket Data Center implements this interface from version
// 7.7 onward.
type PendingReviewGateway interface {
	ReviewGateway

	ReviewContext(context.Context, int64) (ReviewContext, error)
	ReviewAnchor(
		context.Context,
		int64,
		ReviewContext,
		string,
		forge.ReviewThreadRange,
		forge.ReviewThreadSide,
	) (ReviewAnchor, error)
	PublishReview(
		context.Context,
		int64,
		ReviewContext,
		string,
		forge.ReviewDisposition,
	) error
}

// FindChangesOptions filters pull requests
// returned by [Gateway.FindChangesByBranch].
type FindChangesOptions struct {
	// State filters pull requests by their state.
	// Zero means all states.
	State forge.ChangeState

	// PushRepository is the repository that owns the head branch.
	// If nil, only pull requests whose head branch lives
	// in the target repository are returned.
	PushRepository forge.RepositoryID

	// Limit is the maximum number of pull requests to return.
	Limit int
}

// ListCommentsOptions filters comments
// returned by [Gateway.ListComments].
type ListCommentsOptions struct {
	// CanUpdateOnly requests only comments
	// that the current user can update.
	//
	// This filter is best-effort:
	// Bitbucket Cloud cannot filter by author and ignores it.
	CanUpdateOnly bool
}

// Gateway abstracts the REST API differences
// between Bitbucket Cloud and Bitbucket Data Center.
//
// [go.abhg.dev/gs/internal/forge/bitbucket.Repository]
// implements [forge.Repository] on top of this interface,
// keeping all product-specific behavior inside the gateways.
type Gateway interface {
	// Product returns the product name used in user-facing warnings:
	// "Bitbucket" for Cloud, or "Bitbucket Data Center".
	Product() string

	// ChangeURL returns the web URL
	// for viewing the pull request with the given number.
	ChangeURL(number int64) string

	// CreateChange creates a new pull request.
	CreateChange(ctx context.Context, req CreateChangeRequest) (*PullRequest, error)

	// GetChange retrieves a pull request by number.
	GetChange(ctx context.Context, number int64) (*PullRequest, error)

	// ChangeMergeability reports whether a pull request can be merged.
	ChangeMergeability(
		ctx context.Context,
		number int64,
	) (forge.ChangeMergeability, error)

	// FindChangesByBranch lists pull requests
	// whose source branch has the given name.
	FindChangesByBranch(ctx context.Context, branch string, opts FindChangesOptions) ([]*PullRequest, error)

	// UpdateChange modifies an existing pull request.
	UpdateChange(ctx context.Context, number int64, update ChangeUpdate) error

	// SetChangeDraft changes the draft status of a pull request.
	//
	// This is an optional capability:
	// it returns an error matching ErrUnsupported
	// if the product cannot change the draft status after creation.
	SetChangeDraft(ctx context.Context, number int64, draft bool) error

	// MergeChange merges a pull request using the given method.
	MergeChange(ctx context.Context, number int64, method forge.MergeMethod) error

	// ListCommitChecks reports the CI checks
	// recorded for the given commit.
	ListCommitChecks(ctx context.Context, commit git.Hash) ([]forge.ChangeCheck, error)

	// CreateComment posts a new comment on a pull request.
	CreateComment(ctx context.Context, prID int64, body string) (*ChangeComment, error)

	// UpdateComment replaces the body of an existing comment.
	UpdateComment(ctx context.Context, c *ChangeComment, body string) error

	// DeleteComment deletes an existing comment.
	DeleteComment(ctx context.Context, c *ChangeComment) error

	// ListComments lists comments on a pull request.
	ListComments(ctx context.Context, prID int64, opts ListCommentsOptions) iter.Seq2[*ChangeComment, error]

	// ResolvableComments lists review comments on a pull request
	// that participate in comment resolution counts.
	ResolvableComments(ctx context.Context, prID int64) iter.Seq2[*ResolvableComment, error]

	// ChangeTemplate fetches the contents of the change template file
	// at the given path on the repository's default branch.
	//
	// Returns an error matching [forge.ErrNotFound]
	// if the file does not exist.
	ChangeTemplate(ctx context.Context, path string) (string, error)
}
