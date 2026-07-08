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
