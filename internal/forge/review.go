package forge

import (
	"context"
	"fmt"
	"iter"
	"time"

	"go.abhg.dev/gs/internal/git"
)

// ReviewRepository is implemented by repositories that support code reviews.
// A forge may emulate a review submission when it lacks an equivalent native
// operation.
type ReviewRepository interface {
	Repository

	// ListReviewerStates yields each reviewer's latest effective disposition.
	// Reviewers without an effective disposition are omitted.
	// The order is forge-defined.
	ListReviewerStates(
		context.Context,
		ChangeID,
	) iter.Seq2[*ReviewerState, error]

	// ListReviewThreads yields the review threads on a change in forge-defined
	// order.
	ListReviewThreads(
		context.Context,
		ChangeID,
	) iter.Seq2[*ReviewThread, error]

	// SubmitReview publishes the requested body, thread comments,
	// and optional review disposition as one logical operation.
	// If any requested operation is unsupported,
	// it returns an error wrapping [ErrUnsupported]
	// without publishing any part of the request.
	SubmitReview(
		context.Context,
		ChangeID,
		SubmitReviewRequest,
	) (SubmitReviewResult, error)
}

// ReviewerState is one reviewer's latest effective disposition.
type ReviewerState struct {
	// Reviewer is the forge username of the reviewer.
	Reviewer string

	// Disposition is the reviewer's current review outcome.
	// It is either ReviewDispositionApprove
	// or ReviewDispositionRequestChanges.
	Disposition ReviewDisposition

	// CommitHash is the revision that was reviewed.
	// It is zero when the forge does not expose the reviewed revision.
	CommitHash git.Hash

	// SubmittedAt is when the reviewer established this state.
	// It is zero when the forge does not expose the submission time.
	SubmittedAt time.Time
}

// ReviewDisposition specifies the optional outcome attached to a submission.
type ReviewDisposition int

const (
	// ReviewDispositionNone does not establish or change reviewer state.
	ReviewDispositionNone ReviewDisposition = iota

	// ReviewDispositionApprove approves the change.
	ReviewDispositionApprove

	// ReviewDispositionRequestChanges requests changes to the change.
	ReviewDispositionRequestChanges
)

// ReviewThread is a discussion attached to a file
// or line range in a change diff.
type ReviewThread struct {
	// ID identifies the thread within the repository.
	ID ReviewThreadID

	// Path is relative to the repository root.
	Path string

	// Range is the inclusive line range discussed by the thread.
	// A zero range indicates a file-level thread.
	Range ReviewThreadRange

	// Side identifies the revision containing Range.
	// It is ignored when Range is zero.
	Side ReviewThreadSide

	// Resolved reports whether the forge marks the thread as resolved.
	// It is nil when the forge does not expose thread resolution state.
	Resolved *bool

	// Outdated reports whether the thread is attached to an earlier revision.
	// It is nil when the forge does not expose outdated thread state.
	Outdated *bool

	// Comments is ordered from the first comment to the latest reply.
	Comments []ReviewComment
}

// ReviewThreadID identifies a review thread.
//
// Forges without native review threads may synthesize an implementation from
// identifiers that let them find the same discussion in later operations.
type ReviewThreadID interface {
	String() string
}

// ReviewThreadRange identifies an inclusive, one-based line range
// on one side of a diff.
// The zero value identifies the whole file.
// Otherwise, StartLine must be positive
// and no greater than EndLine.
type ReviewThreadRange struct {
	// StartLine is the first line in the range, inclusive.
	// It is zero for a file-level range.
	StartLine int

	// EndLine is the last line in the range, inclusive.
	// It is zero for a file-level range.
	EndLine int
}

// IsZero reports whether the range identifies the whole file.
func (r ReviewThreadRange) IsZero() bool {
	return r == ReviewThreadRange{}
}

// ReviewThreadLine returns a range containing only line.
func ReviewThreadLine(line int) ReviewThreadRange {
	return ReviewThreadRange{
		StartLine: line,
		EndLine:   line,
	}
}

// ReviewThreadSide identifies the revision containing a review thread's
// line range.
type ReviewThreadSide int

const (
	// ReviewThreadSideRight identifies the postimage side of a diff.
	ReviewThreadSideRight ReviewThreadSide = iota

	// ReviewThreadSideLeft identifies the preimage side of a diff.
	ReviewThreadSideLeft
)

// String reports the forge-neutral spelling of the side.
func (s ReviewThreadSide) String() string {
	switch s {
	case ReviewThreadSideRight:
		return "right"
	case ReviewThreadSideLeft:
		return "left"
	default:
		return fmt.Sprintf("ReviewThreadSide(%d)", int(s))
	}
}

// ReviewComment is one comment in a review thread.
type ReviewComment struct {
	// ID identifies the comment within the repository.
	ID ReviewCommentID

	// Body is the comment text as submitted to the forge.
	Body string

	// Author is the forge username of the comment author.
	// It is empty when the forge does not expose the author.
	Author string

	// CreatedAt is when the comment was created.
	// It is zero when the forge does not expose the creation time.
	CreatedAt time.Time
}

// ReviewCommentID identifies one comment in a review thread.
type ReviewCommentID interface {
	String() string
}

// SubmitReviewRequest describes comments and an optional review disposition.
// It must include a body, at least one comment, or a non-zero disposition.
type SubmitReviewRequest struct {
	// Body is optional top-level text to publish.
	Body string

	// Disposition is the optional review outcome to publish.
	// The zero value leaves reviewer state unchanged.
	Disposition ReviewDisposition

	// Comments is the ordered set of thread comments and replies to publish.
	Comments []SubmitReviewCommentRequest
}

// SubmitReviewResult reports the comments published by SubmitReview.
type SubmitReviewResult struct {
	// Comments corresponds positionally to the requested comments.
	Comments []SubmitReviewCommentResult
}

// SubmitReviewCommentRequest describes one thread comment to publish.
type SubmitReviewCommentRequest struct {
	// ReplyTo identifies the thread to which Body is appended.
	// When non-nil, Path, Range, and Side are ignored.
	// When nil, those fields identify the location of a new thread.
	ReplyTo ReviewThreadID

	// Path is relative to the repository root.
	// It is required when ReplyTo is nil and ignored otherwise.
	Path string

	// Range is the inclusive line range to comment on.
	// A zero range requests a file-level comment.
	// It is ignored when ReplyTo is non-nil.
	// If the forge does not support file-level comments,
	// SubmitReview returns an error wrapping [ErrUnsupported]
	// without publishing any part of the request.
	Range ReviewThreadRange

	// Side identifies the revision containing Range.
	// It is ignored when ReplyTo is non-nil or Range is zero.
	Side ReviewThreadSide

	// Body is the comment text to publish and must be non-empty.
	Body string
}

// SubmitReviewCommentResult identifies one comment published by SubmitReview.
type SubmitReviewCommentResult struct {
	// ThreadID identifies the thread containing the comment.
	ThreadID ReviewThreadID

	// CommentID identifies the published comment.
	CommentID ReviewCommentID
}

// ReviewCommentEditor is implemented by review repositories that can update
// review comments after submission.
type ReviewCommentEditor interface {
	ReviewRepository

	// UpdateReviewComment replaces the body of a review comment.
	UpdateReviewComment(context.Context, ReviewCommentID, string) error
}

// ReviewThreadResolver is implemented by review repositories
// that can change the resolution state of review threads.
// Implementations must populate [ReviewThread.Resolved]
// in results from [ReviewRepository.ListReviewThreads].
type ReviewThreadResolver interface {
	ReviewRepository

	// ResolveReviewThread marks a review thread as resolved.
	ResolveReviewThread(context.Context, ReviewThreadID) error

	// UnresolveReviewThread marks a review thread as unresolved.
	UnresolveReviewThread(context.Context, ReviewThreadID) error
}
