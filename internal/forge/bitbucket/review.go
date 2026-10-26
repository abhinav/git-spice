package bitbucket

import (
	"context"
	"fmt"
	"iter"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
)

// Bitbucket exposes review conversations through pull request comments rather
// than a separate thread resource. Each product gateway reconstructs an
// anchored root and its replies in product order.
//
// reviewThreadID preserves the root comment ID and pull request ID.
// The root comment ID is the parent used when posting replies
// and the resource accepted by optional resolve and unresolve operations.
// The pull request ID is retained because every comment endpoint is scoped to
// a pull request, while later ReviewRepository operations receive only the ID
// returned by listing or submission.
// Its String form retains the established "commentID:prID" representation.
//
// reviewCommentID separately preserves each individual comment ID
// and its pull request ID for the pull-request-scoped update endpoint.
// Keeping this concrete type distinct from PRComment prevents ordinary change
// comments from crossing the review-comment editing boundary. Data Center's
// optimistic-locking version travels with this ID without changing its stable
// String representation.
//
// Listing and new-comment submission construct the same synthetic thread
// coordinates.
// Reply submission decodes those coordinates, posts a comment whose parent is
// the root comment, and returns the existing thread ID with the new comment ID.
// Resolve and unresolve decode that same thread ID and operate on its root.
// The retained coordinates therefore reconnect list and submit results to the
// later reply, update, resolve, and unresolve calls that consume them.
//
// Data Center 7.7+ creates requested comments as pending drafts and publishes
// them through its native review endpoint. Before 9.2, it can represent only a
// single line, so the provider anchors a requested range at its start line.
// Thread resolution is exposed separately from 8.9 onward.
type reviewRepository struct {
	*Repository
	reviewGW     gw.ReviewGateway
	capabilities gw.ReviewCapabilities
}

// resolvableReviewRepository keeps ReviewThreadResolver out of the method set
// when the selected Data Center version predates thread resolution.
type resolvableReviewRepository struct {
	*reviewRepository
}

var (
	_ forge.ReviewRepository     = (*reviewRepository)(nil)
	_ forge.ReviewCommentEditor  = (*reviewRepository)(nil)
	_ forge.ReviewThreadResolver = (*resolvableReviewRepository)(nil)
)

// reviewThreadID is a synthetic Bitbucket thread identity.
type reviewThreadID struct {
	CommentID int64
	PRID      int64
}

var _ forge.ReviewThreadID = reviewThreadID{}

func (id reviewThreadID) String() string {
	return strconv.FormatInt(id.CommentID, 10) + ":" +
		strconv.FormatInt(id.PRID, 10)
}

// reviewCommentID identifies one comment in a Bitbucket review thread.
type reviewCommentID struct {
	CommentID int64
	PRID      int64
	Version   int
}

var _ forge.ReviewCommentID = reviewCommentID{}

func (id reviewCommentID) String() string {
	return strconv.FormatInt(id.CommentID, 10) + ":" +
		strconv.FormatInt(id.PRID, 10)
}

// withReviewRepository discovers product capabilities while opening the
// repository. Probe failures are returned because treating an unknown Data
// Center version as unsupported would hide a surface the server may provide.
func withReviewRepository(
	ctx context.Context,
	repo *Repository,
) (forge.Repository, error) {
	reviewGW, ok := repo.gw.(gw.ReviewGateway)
	if !ok {
		return repo, nil
	}
	capabilities, err := reviewGW.ReviewCapabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("determine review capabilities: %w", err)
	}
	if !capabilities.Supported {
		return repo, nil
	}
	reviewRepo := &reviewRepository{
		Repository:   repo,
		reviewGW:     reviewGW,
		capabilities: capabilities,
	}
	if capabilities.ThreadResolution {
		return &resolvableReviewRepository{reviewRepository: reviewRepo}, nil
	}
	return reviewRepo, nil
}

// ListReviewerStates lists the latest state exposed for each participant.
func (r *reviewRepository) ListReviewerStates(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewerState, error] {
	prID := mustPR(id).Number
	return func(yield func(*forge.ReviewerState, error) bool) {
		for state, err := range r.reviewGW.ListReviewerStates(ctx, prID) {
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(&forge.ReviewerState{
				Reviewer:    state.Reviewer,
				Disposition: state.Disposition,
				CommitHash:  state.CommitHash,
				SubmittedAt: state.SubmittedAt,
			}, nil) {
				return
			}
		}
	}
}

// ListReviewThreads lists comment-backed threads on a pull request. Resolved
// is populated only when the selected product version exposes resolution;
// Bitbucket does not expose a distinct outdated-thread flag.
func (r *reviewRepository) ListReviewThreads(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewThread, error] {
	prID := mustPR(id).Number
	return func(yield func(*forge.ReviewThread, error) bool) {
		for thread, err := range r.reviewGW.ListReviewThreads(ctx, prID) {
			if err != nil {
				yield(nil, err)
				return
			}

			item := &forge.ReviewThread{
				ID: reviewThreadID{
					CommentID: thread.RootCommentID,
					PRID:      prID,
				},
				Path:       thread.Path,
				Range:      thread.Range,
				Side:       thread.Side,
				CommitHash: thread.CommitHash,
			}
			if r.capabilities.ThreadResolution {
				resolved := thread.Resolved
				item.Resolved = &resolved
			}
			for _, comment := range thread.Comments {
				item.Comments = append(item.Comments, forge.ReviewComment{
					ID: reviewCommentID{
						CommentID: comment.ID,
						PRID:      prID,
						Version:   comment.Version,
					},
					Body:      comment.Body,
					Author:    comment.Author,
					CreatedAt: comment.CreatedAt,
				})
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// SubmitReview dispatches to the product's review workflow. Cloud publishes
// each resource immediately. Data Center creates pending comments before
// publishing the native review.
func (r *reviewRepository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	mustReviewDisposition(req.Disposition)
	prID := mustPR(id).Number
	if !r.capabilities.FileLevel {
		for _, comment := range req.Comments {
			if comment.ReplyTo == nil && comment.Range.IsZero() {
				return forge.SubmitReviewResult{}, fmt.Errorf(
					"submit file-level comment: %w", forge.ErrUnsupported)
			}
		}
	}
	if r.capabilities.NativeDrafts {
		return r.submitNativeReview(ctx, prID, req)
	}
	return r.submitEmulatedReview(ctx, id, prID, req)
}

// mustReviewDisposition checks the git-spice-owned enum before any provider
// mutation, so an internal invariant failure cannot leave a partial review.
func mustReviewDisposition(disposition forge.ReviewDisposition) {
	switch disposition {
	case forge.ReviewDispositionNone,
		forge.ReviewDispositionApprove,
		forge.ReviewDispositionRequestChanges:
		return
	default:
		panic(fmt.Sprintf(
			"bitbucket: invalid review disposition %d", disposition))
	}
}

// submitEmulatedReview publishes each Cloud resource through its individual
// endpoint before setting the disposition. An error may therefore leave earlier
// operations visible, but SubmitReview does not expose partial results.
func (r *reviewRepository) submitEmulatedReview(
	ctx context.Context,
	id forge.ChangeID,
	prID int64,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	var result forge.SubmitReviewResult
	if req.Body != "" {
		if _, err := r.PostChangeComment(ctx, id, req.Body); err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf(
				"post review body: %w", err)
		}
	}

	for _, comment := range req.Comments {
		posted, err := r.submitReviewComment(
			ctx, prID, gw.ReviewContext{}, comment)
		if err != nil {
			return forge.SubmitReviewResult{}, err
		}
		result.Comments = append(result.Comments, posted)
	}

	if req.Disposition != forge.ReviewDispositionNone {
		dispositionGW, ok := r.reviewGW.(gw.EmulatedReviewGateway)
		if !ok {
			panic(fmt.Sprintf(
				"bitbucket: %T advertises emulated reviews without implementing EmulatedReviewGateway",
				r.reviewGW,
			))
		}
		if err := dispositionGW.SetReviewDisposition(
			ctx, prID, req.Disposition,
		); err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf(
				"set review disposition: %w", err)
		}
	}

	return result, nil
}

// submitNativeReview resolves every Data Center anchor before mutation, creates
// the comments as drafts, and then publishes the native review.
func (r *reviewRepository) submitNativeReview(
	ctx context.Context,
	prID int64,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	pendingGW, ok := r.reviewGW.(gw.PendingReviewGateway)
	if !ok {
		panic(fmt.Sprintf(
			"bitbucket: %T advertises native review drafts without implementing PendingReviewGateway",
			r.reviewGW,
		))
	}

	var reviewContext gw.ReviewContext
	if req.Body != "" || len(req.Comments) > 0 ||
		req.Disposition != forge.ReviewDispositionNone {
		var err error
		reviewContext, err = pendingGW.ReviewContext(ctx, prID)
		if err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf(
				"prepare review: %w", err)
		}
	}

	prepared := make([]gw.CreateReviewCommentRequest, 0, len(req.Comments))
	for _, comment := range req.Comments {
		// Data Center versions before 9.2 have no multiline anchor shape.
		// Preserve the requested start rather than rejecting the whole review.
		if comment.ReplyTo == nil && !comment.Range.IsZero() &&
			!r.capabilities.Multiline {
			comment.Range = forge.ReviewThreadLine(comment.Range.StartLine)
		}
		apiReq := reviewCommentRequest(prID, reviewContext, comment)
		if apiReq.ParentID == 0 {
			anchor, err := pendingGW.ReviewAnchor(
				ctx,
				prID,
				reviewContext,
				apiReq.Path,
				apiReq.Range,
				apiReq.Side,
			)
			if err != nil {
				return forge.SubmitReviewResult{}, fmt.Errorf(
					"prepare review comment: %w", err)
			}
			apiReq.ReviewAnchor = anchor
		}
		prepared = append(prepared, apiReq)
	}

	var pending forge.SubmitReviewResult
	for _, apiReq := range prepared {
		posted, err := r.createReviewComment(ctx, prID, apiReq)
		if err != nil {
			return forge.SubmitReviewResult{}, err
		}
		pending.Comments = append(pending.Comments, posted)
	}

	if req.Body != "" || len(req.Comments) > 0 ||
		req.Disposition != forge.ReviewDispositionNone {
		if err := pendingGW.PublishReview(
			ctx, prID, reviewContext, req.Body, req.Disposition,
		); err != nil {
			return forge.SubmitReviewResult{}, err
		}
	}

	return pending, nil
}

// submitReviewComment converts one forge request and creates it immediately.
// The emulated Cloud path uses this because no pending publication phase exists.
func (r *reviewRepository) submitReviewComment(
	ctx context.Context,
	prID int64,
	reviewContext gw.ReviewContext,
	req forge.SubmitReviewCommentRequest,
) (forge.SubmitReviewCommentResult, error) {
	return r.createReviewComment(
		ctx, prID, reviewCommentRequest(prID, reviewContext, req))
}

// reviewCommentRequest converts git-spice-owned IDs and carries the prepared
// Data Center review context into the gateway request.
func reviewCommentRequest(
	prID int64,
	reviewContext gw.ReviewContext,
	req forge.SubmitReviewCommentRequest,
) gw.CreateReviewCommentRequest {
	apiReq := gw.CreateReviewCommentRequest{
		Path:          req.Path,
		Range:         req.Range,
		Side:          req.Side,
		Body:          req.Body,
		ReviewContext: reviewContext,
	}
	if req.ReplyTo != nil {
		threadID := mustReviewThreadForPullRequest(req.ReplyTo, prID)
		apiReq.ParentID = threadID.CommentID
	}
	return apiReq
}

// createReviewComment reconstructs the stable synthetic thread identity from
// either the new root comment or the parent named by a reply.
func (r *reviewRepository) createReviewComment(
	ctx context.Context,
	prID int64,
	apiReq gw.CreateReviewCommentRequest,
) (forge.SubmitReviewCommentResult, error) {
	comment, err := r.reviewGW.CreateReviewComment(ctx, prID, apiReq)
	if err != nil {
		return forge.SubmitReviewCommentResult{}, err
	}
	threadID := reviewThreadID{CommentID: apiReq.ParentID, PRID: prID}
	if apiReq.ParentID == 0 {
		threadID = reviewThreadID{CommentID: comment.ID, PRID: prID}
	}
	return forge.SubmitReviewCommentResult{
		ThreadID: threadID,
		CommentID: reviewCommentID{
			CommentID: comment.ID,
			PRID:      prID,
			Version:   comment.Version,
		},
	}, nil
}

// mustReviewThreadID converts an ID produced by this provider. A foreign ID is
// an internal wiring error because SubmitReviewRequest is built by git-spice.
func mustReviewThreadID(id forge.ReviewThreadID) reviewThreadID {
	threadID, ok := id.(reviewThreadID)
	if !ok {
		panic(fmt.Sprintf("bitbucket: expected review thread ID, got %T", id))
	}
	return threadID
}

// mustReviewThreadForPullRequest additionally preserves the pull-request scope
// encoded in the synthetic thread ID.
func mustReviewThreadForPullRequest(
	id forge.ReviewThreadID,
	prID int64,
) reviewThreadID {
	threadID := mustReviewThreadID(id)
	if threadID.PRID != prID {
		panic(fmt.Sprintf(
			"review thread %s belongs to pull request %d, not %d",
			threadID, threadID.PRID, prID))
	}
	if threadID.CommentID <= 0 {
		panic(fmt.Sprintf(
			"review thread %s has an invalid comment ID", threadID))
	}
	return threadID
}

// UpdateReviewComment replaces a review comment body.
func (r *reviewRepository) UpdateReviewComment(
	ctx context.Context,
	id forge.ReviewCommentID,
	body string,
) error {
	comment := mustReviewCommentID(id)
	return r.gw.UpdateComment(ctx, &gw.ChangeComment{
		ID:      comment.CommentID,
		PRID:    comment.PRID,
		Version: comment.Version,
	}, body)
}

// mustReviewCommentID keeps ordinary pull request comments from crossing the
// review-comment editing boundary.
func mustReviewCommentID(id forge.ReviewCommentID) reviewCommentID {
	comment, ok := id.(reviewCommentID)
	if !ok {
		panic(fmt.Sprintf("bitbucket: expected review comment ID, got %T", id))
	}
	return comment
}

// ResolveReviewThread resolves a comment-backed thread.
func (r *resolvableReviewRepository) ResolveReviewThread(
	ctx context.Context,
	id forge.ReviewThreadID,
) error {
	threadID := mustReviewThreadID(id)
	return r.reviewGW.ResolveReviewThread(
		ctx, threadID.PRID, threadID.CommentID)
}

// UnresolveReviewThread reopens a comment-backed thread.
func (r *resolvableReviewRepository) UnresolveReviewThread(
	ctx context.Context,
	id forge.ReviewThreadID,
) error {
	threadID := mustReviewThreadID(id)
	return r.reviewGW.UnresolveReviewThread(
		ctx, threadID.PRID, threadID.CommentID)
}
