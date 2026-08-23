package gitea

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
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
	"go.abhg.dev/gs/internal/git"
)

const _reviewPageSize = 50

// Gitea 1.22 exposes pull request reviews and per-review code comments,
// but it has no review-thread resource and no REST reply operation.
// Gitea's UI and pull service group code comments into conversations by issue,
// path, and signed line, so this adapter reconstructs submitted comments across
// reviews under the same pull request, path, and signed line.
// Positive lines select the postimage and negative lines select the preimage.
// A reply is emulated by submitting a new review comment at the thread's saved
// coordinate; consequently, independent comments at that coordinate share the
// same synthetic thread.
// PullReviewComment.Resolver exposes resolution state when present,
// although Gitea 1.22 has no REST route for changing that state.
//
// Gitea 1.22 sources:
// https://github.com/go-gitea/gitea/blob/release/v1.22/modules/structs/pull_review.go
// https://github.com/go-gitea/gitea/blob/release/v1.22/models/issues/review.go
// https://github.com/go-gitea/gitea/blob/release/v1.22/services/pull/review.go
// https://github.com/go-gitea/gitea/blob/release/v1.22/routers/api/v1/repo/pull_review.go
// https://github.com/go-gitea/gitea/blob/release/v1.22/routers/api/v1/api.go
type reviewThreadID struct {
	prNumber int64
	path     string
	line     int64
}

var _ forge.ReviewThreadID = (*reviewThreadID)(nil)

// String includes the pull-request scope and signed diff coordinate that
// distinguish otherwise identical conversations.
func (id *reviewThreadID) String() string {
	return strconv.FormatInt(id.prNumber, 10) + ":" + id.path + ":" +
		strconv.FormatInt(id.line, 10)
}

// mustReviewThreadID unwraps an ID created by this adapter.
// Passing an ID from another forge is a programming error at the shared API
// boundary.
func mustReviewThreadID(id forge.ReviewThreadID) *reviewThreadID {
	thread, ok := id.(*reviewThreadID)
	if !ok {
		panic(fmt.Sprintf("gitea: unexpected review thread type: %T", id))
	}
	return thread
}

// reviewCommentID keeps review comments in a separate identity domain from
// ordinary Gitea change comments, although both use the issue-comment edit API.
type reviewCommentID int64

var _ forge.ReviewCommentID = reviewCommentID(0)

// String reports Gitea's decimal review-comment ID.
func (id reviewCommentID) String() string {
	return strconv.FormatInt(int64(id), 10)
}

// mustReviewCommentID unwraps an ID created by this adapter.
// Passing an ID from another forge is a programming error at the shared API
// boundary.
func mustReviewCommentID(id forge.ReviewCommentID) reviewCommentID {
	comment, ok := id.(reviewCommentID)
	if !ok {
		panic(fmt.Sprintf("gitea: unexpected review comment type: %T", id))
	}
	return comment
}

// ListReviewerStates yields each reviewer's latest effective disposition.
// Reviewers retain the order of their first effective review; submission time
// and then native review ID select the latest state.
func (r *Repository) ListReviewerStates(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewerState, error] {
	return func(yield func(*forge.ReviewerState, error) bool) {
		type latestReview struct {
			id    int64
			state *forge.ReviewerState
		}

		var reviewers []string
		latestByReviewer := make(map[string]latestReview)
		for review, err := range r.listPullReviews(ctx, id) {
			if err != nil {
				yield(nil, err)
				return
			}
			disposition, ok := effectiveReviewDispositionFromGitea(review.State)
			if !ok || review.Dismissed || review.Reviewer == nil ||
				review.Reviewer.Login == "" {
				continue
			}

			reviewer := review.Reviewer.Login
			previous, ok := latestByReviewer[reviewer]
			isLatest := !ok ||
				review.SubmittedAt.After(previous.state.SubmittedAt) ||
				review.SubmittedAt.Equal(previous.state.SubmittedAt) && review.ID > previous.id
			if !isLatest {
				continue
			}
			if !ok {
				reviewers = append(reviewers, reviewer)
			}
			latestByReviewer[reviewer] = latestReview{
				id: review.ID,
				state: &forge.ReviewerState{
					Reviewer:    reviewer,
					Disposition: disposition,
					CommitHash:  git.Hash(review.CommitID),
					SubmittedAt: review.SubmittedAt,
				},
			}
		}

		for _, reviewer := range reviewers {
			if !yield(latestByReviewer[reviewer].state, nil) {
				return
			}
		}
	}
}

// ListReviewThreads yields review conversations grouped by Gitea's
// path-and-signed-line identity.
func (r *Repository) ListReviewThreads(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewThread, error] {
	return func(yield func(*forge.ReviewThread, error) bool) {
		prNumber := mustPR(id).Number
		threadsByID := make(map[reviewThreadID]*forge.ReviewThread)
		var threads []*forge.ReviewThread

		// One conversation may span several submitted reviews, so collect all
		// comments before yielding the PR-wide synthetic threads.
		for review, err := range r.listPullReviews(ctx, id) {
			if err != nil {
				yield(nil, err)
				return
			}
			if review == nil || review.CodeCommentsCount == 0 {
				continue
			}
			// Native COMMENT is not an effective disposition, but it must remain here:
			// Gitea stores inline thread comments on that transport.
			switch review.State {
			case giteagw.ReviewStateApproved,
				giteagw.ReviewStateComment,
				giteagw.ReviewStateRequestChanges:
			default:
				continue
			}

			comments, _, err := r.client.PullReviewCommentList(
				ctx, r.owner, r.repo, prNumber, review.ID,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list review %d comments: %w", review.ID, err))
				return
			}
			for _, comment := range comments {
				threadID, ok := reviewThreadIDFromComment(prNumber, comment)
				if !ok {
					continue
				}
				thread, ok := threadsByID[threadID]
				if !ok {
					thread = newReviewThread(threadID, comment.Resolver != nil)
					threadsByID[threadID] = thread
					threads = append(threads, thread)
				} else if comment.Resolver != nil {
					*thread.Resolved = true
				}
				thread.Comments = append(thread.Comments, reviewCommentFromGitea(comment))
			}
		}

		for _, thread := range threads {
			// Replies from later reviews must follow earlier comments in the
			// reconstructed conversation.
			slices.SortStableFunc(thread.Comments, compareReviewComments)
			if !yield(thread, nil) {
				return
			}
		}
	}
}

// compareReviewComments orders comments chronologically, using the native
// comment ID to make equal timestamps deterministic.
func compareReviewComments(a, b forge.ReviewComment) int {
	if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
		return c
	}
	aID := mustReviewCommentID(a.ID)
	bID := mustReviewCommentID(b.ID)
	return cmp.Compare(aID, bID)
}

// listPullReviews yields every page while preserving Gitea's response order,
// which determines first-seen reviewer and conversation order.
func (r *Repository) listPullReviews(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*giteagw.PullReview, error] {
	return func(yield func(*giteagw.PullReview, error) bool) {
		prNumber := mustPR(id).Number
		opts := &giteagw.ListPullReviewsOptions{
			Limit: _reviewPageSize,
		}
		for page := 1; ; page++ {
			reviews, resp, err := r.client.PullReviewList(
				ctx, r.owner, r.repo, prNumber, opts,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list reviews (page %d): %w", page, err))
				return
			}
			for _, review := range reviews {
				if review == nil {
					continue
				}
				if !yield(review, nil) {
					return
				}
			}
			if resp.NextPage == 0 {
				return
			}
			opts.Page = int64(resp.NextPage)
		}
	}
}

// reviewThreadIDFromComment converts Gitea's old and new position fields to the
// signed line used by synthetic thread IDs. OriginalPosition takes precedence
// because it identifies the preimage side when both fields are populated.
// The boolean is false when the comment has no stable path and line identity.
func reviewThreadIDFromComment(
	prNumber int64,
	comment *giteagw.PullReviewComment,
) (reviewThreadID, bool) {
	if comment == nil {
		return reviewThreadID{}, false
	}
	line := comment.Position
	if comment.OriginalPosition > 0 {
		line = -comment.OriginalPosition
	}
	if comment.Path == "" || line == 0 {
		return reviewThreadID{}, false
	}
	return reviewThreadID{
		prNumber: prNumber,
		path:     comment.Path,
		line:     line,
	}, true
}

// newReviewThread expands a signed synthetic ID into the forge-neutral side and
// line range while preserving that ID for later replies. Resolved is non-nil
// because Gitea exposes resolver presence even without a thread resource.
func newReviewThread(id reviewThreadID, resolved bool) *forge.ReviewThread {
	side := forge.ReviewThreadSideRight
	line := id.line
	if line < 0 {
		side = forge.ReviewThreadSideLeft
		line = -line
	}
	return &forge.ReviewThread{
		ID: &reviewThreadID{
			prNumber: id.prNumber,
			path:     id.path,
			line:     id.line,
		},
		Path:     id.path,
		Range:    forge.ReviewThreadLine(int(line)),
		Side:     side,
		Resolved: new(resolved),
	}
}

// reviewCommentFromGitea preserves the review-comment ID domain and maps a
// missing provider user to the forge contract's empty author.
func reviewCommentFromGitea(comment *giteagw.PullReviewComment) forge.ReviewComment {
	author := ""
	if comment.User != nil {
		author = comment.User.Login
	}
	return forge.ReviewComment{
		ID:        reviewCommentID(comment.ID),
		Body:      comment.Body,
		Author:    author,
		CreatedAt: comment.CreatedAt,
	}
}

// SubmitReview creates one native Gitea review. Gitea 1.22 has no REST reply
// operation, so replies are created at the path and signed line encoded in the
// synthetic thread ID. Gitea groups those comments into the same conversation.
func (r *Repository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	prNumber := mustPR(id).Number
	body, err := reviewBodyForGitea(req)
	if err != nil {
		return forge.SubmitReviewResult{}, err
	}

	comments := make([]giteagw.CreatePullReviewCommentOptions, 0, len(req.Comments))
	threadIDs := make([]*reviewThreadID, 0, len(req.Comments))
	for _, comment := range req.Comments {
		threadID := mustReviewThreadIDForRequest(prNumber, comment)
		comments = append(comments, createReviewCommentOptions(threadID, comment.Body))
		threadIDs = append(threadIDs, threadID)
	}

	review, _, err := r.client.PullReviewCreate(
		ctx,
		r.owner,
		r.repo,
		prNumber,
		&giteagw.CreatePullReviewOptions{
			Event:    mustReviewDispositionToGitea(req.Disposition),
			Body:     body,
			Comments: comments,
		},
	)
	if err != nil {
		return forge.SubmitReviewResult{}, fmt.Errorf("submit review: %w", err)
	}
	if len(comments) == 0 {
		return forge.SubmitReviewResult{}, nil
	}

	// Review creation omits the new comment IDs. Read the review back and
	// correlate each response comment to its request coordinate and body.
	created, _, err := r.client.PullReviewCommentList(
		ctx, r.owner, r.repo, prNumber, review.ID,
	)
	if err != nil {
		return forge.SubmitReviewResult{}, fmt.Errorf("list submitted review comments: %w", err)
	}

	results := make([]forge.SubmitReviewCommentResult, len(req.Comments))
	used := make(map[int64]struct{}, len(created))
	for i, request := range req.Comments {
		comment := findReviewComment(created, used, threadIDs[i], request.Body)
		if comment == nil {
			return forge.SubmitReviewResult{}, fmt.Errorf(
				"identify submitted review comment %d", i+1,
			)
		}
		used[comment.ID] = struct{}{}
		results[i] = forge.SubmitReviewCommentResult{
			ThreadID:  threadIDs[i],
			CommentID: reviewCommentID(comment.ID),
		}
	}
	return forge.SubmitReviewResult{Comments: results}, nil
}

// reviewBodyForGitea supplies text for review events that Gitea 1.22 cannot
// represent without a body. A COMMENT event still needs caller content,
// while a request for changes can degrade to a provider-owned explanation.
func reviewBodyForGitea(req forge.SubmitReviewRequest) (string, error) {
	if strings.TrimSpace(req.Body) != "" {
		return req.Body, nil
	}
	if req.Disposition == forge.ReviewDispositionNone && len(req.Comments) == 0 {
		return "", errors.New(
			"submit review: Gitea 1.22 review event COMMENT requires a body or comment",
		)
	}
	if req.Disposition == forge.ReviewDispositionRequestChanges {
		return "Requesting changes", nil
	}
	return req.Body, nil
}

// mustReviewThreadIDForRequest preserves a reply's stored identity or derives a
// root identity from pull request, path, and signed requested line. It panics if
// an internally constructed request violates provider ID, scope, or side
// invariants.
func mustReviewThreadIDForRequest(
	prNumber int64,
	comment forge.SubmitReviewCommentRequest,
) *reviewThreadID {
	if comment.ReplyTo != nil {
		thread := mustReviewThreadID(comment.ReplyTo)
		if thread.prNumber != prNumber {
			panic(fmt.Sprintf(
				"gitea: review thread belongs to pull request %d, not %d",
				thread.prNumber,
				prNumber,
			))
		}
		return thread
	}
	// Gitea 1.22 accepts only one line per review comment. Preserve a range's
	// first line so callers still get an anchored comment instead of an error.
	line := int64(comment.Range.StartLine)
	switch comment.Side {
	case forge.ReviewThreadSideRight:
	case forge.ReviewThreadSideLeft:
		line = -line
	default:
		panic(fmt.Sprintf("gitea: unsupported review thread side %s", comment.Side))
	}
	return &reviewThreadID{
		prNumber: prNumber,
		path:     comment.Path,
		line:     line,
	}
}

// findReviewComment returns the unused response comment matching one request's
// synthetic thread coordinate and body. used disambiguates duplicate requests
// with identical contents and coordinates.
func findReviewComment(
	comments []*giteagw.PullReviewComment,
	used map[int64]struct{},
	wantID *reviewThreadID,
	wantBody string,
) *giteagw.PullReviewComment {
	for _, comment := range comments {
		if comment == nil || comment.Body != wantBody {
			continue
		}
		if _, ok := used[comment.ID]; ok {
			continue
		}
		id, ok := reviewThreadIDFromComment(wantID.prNumber, comment)
		if ok && id == *wantID {
			return comment
		}
	}
	return nil
}

// createReviewCommentOptions converts the signed synthetic line back to
// Gitea's mutually exclusive old_position and new_position fields.
func createReviewCommentOptions(
	id *reviewThreadID,
	body string,
) giteagw.CreatePullReviewCommentOptions {
	comment := giteagw.CreatePullReviewCommentOptions{
		Path: id.path,
		Body: body,
	}
	if id.line < 0 {
		comment.OldPosition = -id.line
	} else {
		comment.NewPosition = id.line
	}
	return comment
}

// mustReviewDispositionToGitea converts the shared closed disposition set to a
// Gitea review event. Unknown values violate the shared API invariant.
func mustReviewDispositionToGitea(
	disposition forge.ReviewDisposition,
) giteagw.ReviewState {
	switch disposition {
	case forge.ReviewDispositionNone:
		return giteagw.ReviewStateComment
	case forge.ReviewDispositionApprove:
		return giteagw.ReviewStateApproved
	case forge.ReviewDispositionRequestChanges:
		return giteagw.ReviewStateRequestChanges
	default:
		panic(fmt.Sprintf("gitea: unsupported review disposition %d", disposition))
	}
}

// effectiveReviewDispositionFromGitea translates states that establish or
// change reviewer state.
func effectiveReviewDispositionFromGitea(
	state giteagw.ReviewState,
) (forge.ReviewDisposition, bool) {
	switch state {
	case giteagw.ReviewStateApproved:
		return forge.ReviewDispositionApprove, true
	case giteagw.ReviewStateRequestChanges:
		return forge.ReviewDispositionRequestChanges, true
	default:
		return forge.ReviewDispositionNone, false
	}
}

// UpdateReviewComment replaces a Gitea review comment's body.
func (r *Repository) UpdateReviewComment(
	ctx context.Context,
	id forge.ReviewCommentID,
	body string,
) error {
	commentID := mustReviewCommentID(id)
	// Gitea exposes review comments through the general issue-comment edit
	// endpoint even though listing and creation are review-scoped.
	if _, _, err := r.client.CommentEdit(
		ctx, r.owner, r.repo, int64(commentID), body,
	); err != nil {
		if errors.Is(err, giteagw.ErrNotFound) {
			return fmt.Errorf("update review comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update review comment: %w", err)
	}
	return nil
}
