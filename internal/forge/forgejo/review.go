package forgejo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/forgejo"
	"go.abhg.dev/gs/internal/git"
)

// Forgejo v11 does not expose a first-class review-thread object.
// A review owns code comments,
// and Forgejo groups submitted comments by review ID, path, and signed line:
// positive lines are on the right side and negative lines are on the left.
// Comments at the same coordinates in one review form one discussion,
// even though Forgejo does not represent a parent/reply relationship.
//
// reviewThreadID preserves the pull-request scope
// and those three grouping coordinates.
// Listing reconstructs the same ID from submitted reviews and their comments.
// New roots are created with a native review,
// then read back to recover Forgejo's comment IDs.
// Replies post another comment at the stored path and signed line
// through an endpoint scoped by both pull-request and review ID.
// Forgejo permits any authenticated user to append to a submitted review;
// another user's pending review is inaccessible and is never listed here.
//
// Forgejo exposes a resolver on listed comments,
// so grouped threads report resolution state.
// Forgejo v11 has no REST operation to change that state,
// so Repository deliberately does not implement forge.ReviewThreadResolver.
var (
	_ forge.ReviewRepository    = (*Repository)(nil)
	_ forge.ReviewCommentEditor = (*Repository)(nil)
)

// reviewThreadID carries the pull request, review, and diff coordinates
// Forgejo requires to append another comment to the same code-comment group.
type reviewThreadID struct {
	prNumber int64
	reviewID int64
	path     string
	position int64
}

var _ forge.ReviewThreadID = (*reviewThreadID)(nil)

// mustReviewThreadID unwraps an ID created by this adapter.
// Passing an ID from another forge is a programming error at the shared API
// boundary.
func mustReviewThreadID(id forge.ReviewThreadID) *reviewThreadID {
	thread, ok := id.(*reviewThreadID)
	if !ok {
		panic(fmt.Sprintf("forgejo: expected *reviewThreadID, got %T", id))
	}
	return thread
}

// String includes the provider coordinates needed to distinguish discussions
// at the same path and line across pull requests and native reviews.
func (id *reviewThreadID) String() string {
	return fmt.Sprintf(
		"%d:%d:%s:%d",
		id.prNumber,
		id.reviewID,
		id.path,
		id.position,
	)
}

// reviewCommentID preserves the native comment ID used by Forgejo's shared
// issue-comment edit endpoint.
type reviewCommentID int64

var _ forge.ReviewCommentID = reviewCommentID(0)

// mustReviewCommentID unwraps an ID created by this adapter.
// Passing an ID from another forge is a programming error at the shared API
// boundary.
func mustReviewCommentID(id forge.ReviewCommentID) reviewCommentID {
	comment, ok := id.(reviewCommentID)
	if !ok {
		panic(fmt.Sprintf("forgejo: expected reviewCommentID, got %T", id))
	}
	return comment
}

// String reports Forgejo's decimal comment ID.
func (id reviewCommentID) String() string {
	return strconv.FormatInt(int64(id), 10)
}

// ListReviewerStates lists the latest effective review from each reviewer.
// Reviewers retain the order in which their first effective review appears;
// submission time and then native review ID select their latest state.
func (r *Repository) ListReviewerStates(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewerState, error] {
	prNumber := mustPR(id).Number
	return func(yield func(*forge.ReviewerState, error) bool) {
		states := make(map[string]*reviewerState)
		var reviewers []string
		for review, err := range r.listPullReviews(ctx, prNumber) {
			if err != nil {
				yield(nil, err)
				return
			}
			state, ok := reviewerStateFromPullReview(review)
			if !ok {
				continue
			}

			current, ok := states[state.Reviewer]
			if !ok {
				reviewers = append(reviewers, state.Reviewer)
			} else if state.SubmittedAt.Before(current.SubmittedAt) ||
				state.SubmittedAt.Equal(current.SubmittedAt) && state.reviewID < current.reviewID {
				continue
			}
			states[state.Reviewer] = state
		}

		for _, reviewer := range reviewers {
			state := states[reviewer]
			if !yield(&state.ReviewerState, nil) {
				return
			}
		}
	}
}

// reviewerState retains the native ID used to break ties between reviews with
// identical submission timestamps.
type reviewerState struct {
	forge.ReviewerState
	reviewID int64
}

// reviewerStateFromPullReview converts an effective submitted review.
// Pending, dismissed, anonymous, and provider-only states do not establish a
// reviewer's current disposition.
func reviewerStateFromPullReview(review *forgejo.PullReview) (*reviewerState, bool) {
	if review == nil || review.User == nil || review.User.Login == "" || review.Dismissed {
		return nil, false
	}

	disposition, ok := reviewDisposition(review.State)
	if !ok {
		return nil, false
	}

	return &reviewerState{
		Reviewer:    review.User.Login,
		Disposition: disposition,
		CommitHash:  git.Hash(review.CommitID),
		SubmittedAt: review.SubmittedAt,
		reviewID:    review.ID,
	}, true
}

// reviewDisposition translates effective reviewer states.
// COMMENT is a submitted transport for bodies and inline comments,
// but does not establish a shared reviewer disposition.
func reviewDisposition(
	state forgejo.PullReviewState,
) (forge.ReviewDisposition, bool) {
	switch state {
	case forgejo.PullReviewStateApproved:
		return forge.ReviewDispositionApprove, true
	case forgejo.PullReviewStateRequestChanges:
		return forge.ReviewDispositionRequestChanges, true
	default:
		return 0, false
	}
}

// ListReviewThreads lists the code-comment groups attached to a pull request.
// Native review order follows Forgejo's paginated response; threads within a
// review are ordered by their first comment.
func (r *Repository) ListReviewThreads(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewThread, error] {
	prNumber := mustPR(id).Number
	return func(yield func(*forge.ReviewThread, error) bool) {
		for review, err := range r.listPullReviews(ctx, prNumber) {
			if err != nil {
				yield(nil, err)
				return
			}
			if review == nil {
				continue
			}
			if !isSubmittedReviewState(review.State) {
				continue
			}

			comments, _, err := r.client.PullReviewCommentList(
				ctx, r.owner, r.repo, prNumber, review.ID, nil,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list review %d comments: %w", review.ID, err))
				return
			}

			// Forgejo does not guarantee comment order. Normalize it before
			// grouping so both thread order and reply order remain stable.
			slices.SortStableFunc(comments, compareReviewComments)
			threads := groupReviewComments(prNumber, review, comments)
			for _, thread := range threads {
				if !yield(thread, nil) {
					return
				}
			}
		}
	}
}

// isSubmittedReviewState reports whether Forgejo exposes comments for the
// native review after submission.
func isSubmittedReviewState(state forgejo.PullReviewState) bool {
	switch state {
	case forgejo.PullReviewStateComment,
		forgejo.PullReviewStateApproved,
		forgejo.PullReviewStateRequestChanges:
		return true
	default:
		return false
	}
}

// compareReviewComments orders comments chronologically, using the native ID
// to make equal timestamps deterministic.
func compareReviewComments(a, b *forgejo.PullReviewComment) int {
	if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

// groupReviewComments reconstructs threads for one native review.
// The grouping key is pull request, review, path, and signed position, where a
// positive position is on the right side and a negative position is on the
// left. The input must already be sorted; the first occurrence fixes thread
// order, and append order preserves comment order within each thread.
// Resolution is aggregated with OR because Forgejo exposes the resolver on
// comments rather than on a first-class thread.
func groupReviewComments(
	prNumber int64,
	review *forgejo.PullReview,
	comments []*forgejo.PullReviewComment,
) []*forge.ReviewThread {
	byID := make(map[reviewThreadID]*forge.ReviewThread)
	threads := make([]*forge.ReviewThread, 0, len(comments))
	outdated := review.Stale
	for _, comment := range comments {
		position, side, ok := reviewCommentPosition(comment)
		if !ok || comment.Path == "" {
			continue
		}

		key := reviewThreadID{
			prNumber: prNumber,
			reviewID: review.ID,
			path:     comment.Path,
			position: position,
		}
		thread, ok := byID[key]
		if !ok {
			resolved := comment.Resolver != nil
			threadID := key
			thread = &forge.ReviewThread{
				ID:       &threadID,
				Path:     comment.Path,
				Range:    forge.ReviewThreadLine(int(max(position, -position))),
				Side:     side,
				Resolved: &resolved,
				Outdated: &outdated,
			}
			byID[key] = thread
			threads = append(threads, thread)
		} else if comment.Resolver != nil {
			*thread.Resolved = true
		}

		author := ""
		if comment.User != nil {
			author = comment.User.Login
		}
		thread.Comments = append(thread.Comments, forge.ReviewComment{
			ID:        reviewCommentID(comment.ID),
			Body:      comment.Body,
			Author:    author,
			CreatedAt: comment.CreatedAt,
		})
	}
	return threads
}

// reviewCommentPosition converts Forgejo's separate old and new positions to
// the signed position used by synthetic thread IDs.
func reviewCommentPosition(
	comment *forgejo.PullReviewComment,
) (position int64, side forge.ReviewThreadSide, ok bool) {
	if comment.Position > 0 {
		return comment.Position, forge.ReviewThreadSideRight, true
	}
	if comment.OriginalPosition > 0 {
		return -comment.OriginalPosition, forge.ReviewThreadSideLeft, true
	}
	return 0, 0, false
}

// SubmitReview publishes root comments in one native Forgejo review, then
// appends replies through the per-review comment endpoint.
func (r *Repository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	for _, comment := range req.Comments {
		if comment.ReplyTo == nil && comment.Range.IsZero() {
			return forge.SubmitReviewResult{}, fmt.Errorf(
				"submit file-level comment: %w", forge.ErrUnsupported)
		}
	}

	prNumber := mustPR(id).Number

	results := make([]forge.SubmitReviewCommentResult, len(req.Comments))
	rootIndexes := make([]int, 0, len(req.Comments))
	createOptions := forgejo.CreatePullReviewOptions{
		Body:  req.Body,
		Event: mustPullReviewState(req.Disposition),
	}
	for i, comment := range req.Comments {
		if comment.ReplyTo != nil {
			continue
		}
		rootIndexes = append(rootIndexes, i)
		createOptions.Comments = append(
			createOptions.Comments,
			mustCreatePullReviewCommentOptions(comment),
		)
	}
	if req.Disposition == forge.ReviewDispositionRequestChanges &&
		strings.TrimSpace(req.Body) == "" && len(rootIndexes) == 0 {
		// Forgejo rejects a change request without a top-level body or root
		// comment. Supply a neutral body so callers retain the requested
		// disposition instead of receiving a provider-specific error.
		createOptions.Body = "Requesting changes"
	}

	if req.Body != "" || req.Disposition != forge.ReviewDispositionNone || len(rootIndexes) > 0 {
		review, _, err := r.client.PullReviewCreate(
			ctx, r.owner, r.repo, prNumber, &createOptions,
		)
		if err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf("create review: %w", err)
		}
		if len(rootIndexes) > 0 {
			// The create response identifies the native review but omits its
			// comments. Read roots back before posting replies to recover comment
			// IDs and the signed coordinates needed for stable thread IDs.
			comments, _, err := r.client.PullReviewCommentList(
				ctx, r.owner, r.repo, prNumber, review.ID, nil,
			)
			if err != nil {
				return forge.SubmitReviewResult{}, fmt.Errorf("list created review comments: %w", err)
			}
			used := make(map[int64]struct{}, len(comments))
			for _, index := range rootIndexes {
				request := req.Comments[index]
				comment := findReviewComment(comments, used, request)
				if comment == nil {
					return forge.SubmitReviewResult{}, fmt.Errorf(
						"identify created review comment at %s:%d",
						request.Path, request.Range.StartLine,
					)
				}
				used[comment.ID] = struct{}{}
				position, _, _ := reviewCommentPosition(comment)
				results[index] = forge.SubmitReviewCommentResult{
					ThreadID: &reviewThreadID{
						prNumber: prNumber,
						reviewID: review.ID,
						path:     comment.Path,
						position: position,
					},
					CommentID: reviewCommentID(comment.ID),
				}
			}
		}
	}

	// Replies cannot be included in Forgejo's native review creation.
	// Publish them only after root readback so mixed requests recover every
	// root ID before the first independently failing reply operation.
	for i, request := range req.Comments {
		if request.ReplyTo == nil {
			continue
		}
		thread := mustReviewThreadID(request.ReplyTo)
		options := forgejo.CreatePullReviewCommentOptions{
			Body: request.Body,
			Path: thread.path,
		}
		if thread.position > 0 {
			options.NewPosition = thread.position
		} else {
			options.OldPosition = -thread.position
		}
		comment, _, err := r.client.PullReviewCommentCreate(
			ctx,
			r.owner,
			r.repo,
			prNumber,
			thread.reviewID,
			&options,
		)
		if err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf("create review reply: %w", err)
		}
		results[i] = forge.SubmitReviewCommentResult{
			ThreadID:  request.ReplyTo,
			CommentID: reviewCommentID(comment.ID),
		}
	}

	return forge.SubmitReviewResult{Comments: results}, nil
}

// mustPullReviewState converts the shared closed disposition set to Forgejo's
// native event. Unknown values violate the shared API invariant.
func mustPullReviewState(disposition forge.ReviewDisposition) forgejo.PullReviewState {
	switch disposition {
	case forge.ReviewDispositionNone:
		return forgejo.PullReviewStateComment
	case forge.ReviewDispositionApprove:
		return forgejo.PullReviewStateApproved
	case forge.ReviewDispositionRequestChanges:
		return forgejo.PullReviewStateRequestChanges
	default:
		panic(fmt.Sprintf("forgejo: unsupported review disposition %d", disposition))
	}
}

// mustCreatePullReviewCommentOptions converts a root comment to Forgejo's
// side-specific coordinate fields. Unknown sides violate the shared API
// invariant.
func mustCreatePullReviewCommentOptions(
	comment forge.SubmitReviewCommentRequest,
) forgejo.CreatePullReviewCommentOptions {
	// Forgejo v11 accepts only one coordinate for a review comment. Anchor a
	// range at its first line so the comment remains useful on this provider.
	options := forgejo.CreatePullReviewCommentOptions{
		Body: comment.Body,
		Path: comment.Path,
	}
	switch comment.Side {
	case forge.ReviewThreadSideLeft:
		options.OldPosition = int64(comment.Range.StartLine)
	case forge.ReviewThreadSideRight:
		options.NewPosition = int64(comment.Range.StartLine)
	default:
		panic(fmt.Sprintf("forgejo: unsupported review thread side %s", comment.Side))
	}
	return options
}

// findReviewComment matches a created root by all coordinates returned from
// Forgejo. used disambiguates duplicate requests with identical contents.
func findReviewComment(
	comments []*forgejo.PullReviewComment,
	used map[int64]struct{},
	request forge.SubmitReviewCommentRequest,
) *forgejo.PullReviewComment {
	for _, comment := range comments {
		if _, ok := used[comment.ID]; ok {
			continue
		}
		if comment.Body != request.Body || comment.Path != request.Path {
			continue
		}
		position, side, ok := reviewCommentPosition(comment)
		if ok && side == request.Side && max(position, -position) == int64(request.Range.StartLine) {
			return comment
		}
	}
	return nil
}

// listPullReviews streams reviews in Forgejo's response order.
// It fetches the next page only after the consumer accepts every review from
// the current page, so an early stop does not retain or request unused pages.
func (r *Repository) listPullReviews(
	ctx context.Context,
	prNumber int64,
) iter.Seq2[*forgejo.PullReview, error] {
	return func(yield func(*forgejo.PullReview, error) bool) {
		options := &forgejo.ListOptions{Page: 1, Limit: 100}
		for {
			reviews, response, err := r.client.PullReviewList(
				ctx, r.owner, r.repo, prNumber, options,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list reviews: %w", err))
				return
			}
			for _, review := range reviews {
				if !yield(review, nil) {
					return
				}
			}
			if response.NextPage == 0 {
				return
			}
			options.Page = int64(response.NextPage)
		}
	}
}

// UpdateReviewComment replaces the body of a review comment.
func (r *Repository) UpdateReviewComment(
	ctx context.Context,
	id forge.ReviewCommentID,
	body string,
) error {
	commentID := mustReviewCommentID(id)
	// Forgejo exposes review comments through the general issue-comment edit
	// endpoint even though listing and creation are review-scoped.
	_, _, err := r.client.IssueCommentEdit(
		ctx,
		r.owner,
		r.repo,
		int64(commentID),
		&forgejo.EditIssueCommentOption{Body: body},
	)
	if err != nil {
		if errors.Is(err, forgejo.ErrNotFound) {
			return fmt.Errorf("update review comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update review comment: %w", err)
	}
	return nil
}
