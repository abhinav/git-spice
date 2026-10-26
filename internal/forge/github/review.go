package github

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/git"
)

// PRReviewThread identifies a GitHub pull request review thread.
type PRReviewThread struct {
	// GQLID is the review thread's GraphQL node ID.
	GQLID github.ID `json:"gqlID"`
}

// PRReviewComment identifies a GitHub pull request review comment.
type PRReviewComment struct {
	// GQLID is the review comment's GraphQL node ID.
	GQLID github.ID `json:"gqlID"`

	// URL is GitHub's browser URL for the review comment.
	URL string `json:"url"`
}

var (
	_ forge.ReviewRepository     = (*Repository)(nil)
	_ forge.ReviewCommentEditor  = (*Repository)(nil)
	_ forge.ReviewThreadResolver = (*Repository)(nil)
	_ forge.ReviewThreadID       = (*PRReviewThread)(nil)
	_ forge.ReviewCommentID      = (*PRReviewComment)(nil)
)

func (id *PRReviewThread) String() string { return string(id.GQLID) }

func (id *PRReviewComment) String() string { return id.URL }

// ListReviewerStates yields each reviewer's latest effective submitted review.
func (r *Repository) ListReviewerStates(ctx context.Context, id forge.ChangeID) iter.Seq2[*forge.ReviewerState, error] {
	gqlID, err := r.graphQLID(ctx, mustPR(id))
	if err != nil {
		return func(yield func(*forge.ReviewerState, error) bool) {
			yield(nil, err)
		}
	}
	return func(yield func(*forge.ReviewerState, error) bool) {
		for review, err := range r.gateway.PullRequestLatestOpinionatedReviews(ctx, gqlID, nil) {
			if err != nil {
				yield(nil, err)
				return
			}
			disposition, effective := reviewDisposition(review.State)
			if !effective {
				continue
			}
			if !yield(&forge.ReviewerState{
				Reviewer:    review.Author.Login,
				Disposition: disposition,
				CommitHash:  git.Hash(review.Commit.OID),
				SubmittedAt: review.SubmittedAt,
			}, nil) {
				return
			}
		}
	}
}

// ListReviewThreads yields every review thread and its comments.
func (r *Repository) ListReviewThreads(ctx context.Context, id forge.ChangeID) iter.Seq2[*forge.ReviewThread, error] {
	gqlID, err := r.graphQLID(ctx, mustPR(id))
	if err != nil {
		return func(yield func(*forge.ReviewThread, error) bool) {
			yield(nil, err)
		}
	}
	return func(yield func(*forge.ReviewThread, error) bool) {
		for thread, err := range r.gateway.PullRequestReviewThreads(ctx, gqlID, nil) {
			if err != nil {
				yield(nil, err)
				return
			}
			var (
				threadRange forge.ReviewThreadRange
				threadSide  forge.ReviewThreadSide
			)
			if thread.SubjectType != github.ReviewThreadSubjectTypeFile {
				endLine := reviewLine(thread.Line, thread.OriginalLine, 0)
				threadRange = forge.ReviewThreadRange{
					StartLine: reviewLine(thread.StartLine, thread.OriginalStartLine, endLine),
					EndLine:   endLine,
				}
				threadSide = reviewThreadSide(thread.DiffSide)
			}
			comments := make([]forge.ReviewComment, len(thread.Comments))
			for i, comment := range thread.Comments {
				comments[i] = forge.ReviewComment{
					ID:        &PRReviewComment{GQLID: comment.ID, URL: comment.URL},
					Body:      comment.Body,
					Author:    comment.Author.Login,
					CreatedAt: comment.CreatedAt,
				}
			}
			resolved, outdated := thread.IsResolved, thread.IsOutdated
			var commitHash git.Hash
			if len(thread.Comments) > 0 {
				commitHash = git.Hash(thread.Comments[0].OriginalCommit.OID)
			}
			if !yield(&forge.ReviewThread{
				ID:         &PRReviewThread{GQLID: thread.ID},
				Path:       thread.Path,
				Range:      threadRange,
				Side:       threadSide,
				CommitHash: commitHash,
				Resolved:   &resolved,
				Outdated:   &outdated,
				Comments:   comments,
			}, nil) {
				return
			}
		}
	}
}

// SubmitReview publishes the requested body and comments.
// Effective dispositions use one pending review; content-only submissions use
// GitHub's direct comment mutations without submitting a COMMENT review.
func (r *Repository) SubmitReview(ctx context.Context, id forge.ChangeID, req forge.SubmitReviewRequest) (forge.SubmitReviewResult, error) {
	gqlID, err := r.graphQLID(ctx, mustPR(id))
	if err != nil {
		return forge.SubmitReviewResult{}, err
	}

	var (
		event    github.ReviewEvent
		reviewID github.ID
	)
	switch req.Disposition {
	case forge.ReviewDispositionNone:
		if req.Body != "" {
			if _, err := r.gateway.AddComment(ctx, gqlID, req.Body); err != nil {
				return forge.SubmitReviewResult{}, fmt.Errorf("post pull request comment: %w", err)
			}
		}
	case forge.ReviewDispositionApprove, forge.ReviewDispositionRequestChanges:
		event = reviewEvent(req.Disposition)
		review, err := r.gateway.AddPullRequestReview(ctx, &github.AddPullRequestReviewInput{PullRequestID: gqlID})
		if err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf("start review: %w", err)
		}
		reviewID = review.ID
	default:
		panic(fmt.Sprintf("unexpected review disposition: %v", req.Disposition))
	}

	comments := make([]forge.SubmitReviewCommentResult, 0, len(req.Comments))
	for _, comment := range req.Comments {
		if comment.ReplyTo != nil {
			threadID := mustPRReviewThread(comment.ReplyTo).GQLID
			reply, err := r.gateway.AddPullRequestReviewThreadReply(ctx, &github.AddPullRequestReviewThreadReplyInput{
				PullRequestReviewThreadID: threadID,
				PullRequestReviewID:       reviewID,
				Body:                      comment.Body,
			})
			if err != nil {
				return forge.SubmitReviewResult{}, fmt.Errorf("add review reply: %w", err)
			}
			comments = append(comments, forge.SubmitReviewCommentResult{
				ThreadID:  &PRReviewThread{GQLID: threadID},
				CommentID: &PRReviewComment{GQLID: reply.ID, URL: reply.URL},
			})
			continue
		}

		input := &github.AddPullRequestReviewThreadInput{
			PullRequestReviewID: reviewID,
			Path:                comment.Path,
			Body:                comment.Body,
		}
		if reviewID == "" {
			// Content-only threads target the pull request directly;
			// disposition-bearing threads attach to the pending review.
			input.PullRequestID = gqlID
		}
		if comment.Range.IsZero() {
			input.SubjectType = github.ReviewThreadSubjectTypeFile
		} else {
			input.Line = comment.Range.EndLine
			input.Side = diffSide(comment.Side)
			if comment.Range.StartLine != comment.Range.EndLine {
				input.StartLine = &comment.Range.StartLine
				input.StartSide = &input.Side
			}
		}
		thread, err := r.gateway.AddPullRequestReviewThread(ctx, input)
		if err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf("add review thread: %w", err)
		}
		comments = append(comments, forge.SubmitReviewCommentResult{
			ThreadID:  &PRReviewThread{GQLID: thread.ID},
			CommentID: &PRReviewComment{GQLID: thread.Comment.ID, URL: thread.Comment.URL},
		})
	}

	if reviewID != "" {
		if err := r.gateway.SubmitPullRequestReview(ctx, &github.SubmitPullRequestReviewInput{
			PullRequestReviewID: reviewID,
			Event:               event,
			Body:                req.Body,
		}); err != nil {
			return forge.SubmitReviewResult{}, fmt.Errorf("submit review: %w", err)
		}
	}
	r.log.Debug("Published review content", "change", id.String(), "comments", len(comments))
	return forge.SubmitReviewResult{Comments: comments}, nil
}

// UpdateReviewComment replaces a GitHub review comment body.
func (r *Repository) UpdateReviewComment(ctx context.Context, id forge.ReviewCommentID, body string) error {
	comment := mustPRReviewComment(id)
	if err := r.gateway.UpdatePullRequestReviewComment(ctx, comment.GQLID, body); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return fmt.Errorf("update review comment: %w", forge.ErrNotFound)
		}
		return fmt.Errorf("update review comment: %w", err)
	}
	return nil
}

// ResolveReviewThread marks a GitHub review thread resolved.
func (r *Repository) ResolveReviewThread(ctx context.Context, id forge.ReviewThreadID) error {
	if err := r.gateway.ResolveReviewThread(ctx, mustPRReviewThread(id).GQLID); err != nil {
		return fmt.Errorf("resolve review thread: %w", err)
	}
	return nil
}

// UnresolveReviewThread marks a GitHub review thread unresolved.
func (r *Repository) UnresolveReviewThread(ctx context.Context, id forge.ReviewThreadID) error {
	if err := r.gateway.UnresolveReviewThread(ctx, mustPRReviewThread(id).GQLID); err != nil {
		return fmt.Errorf("unresolve review thread: %w", err)
	}
	return nil
}

// mustPRReviewThread narrows a forge review thread ID to GitHub's ID type.
func mustPRReviewThread(id forge.ReviewThreadID) *PRReviewThread {
	if id == nil {
		return nil
	}
	thread, ok := id.(*PRReviewThread)
	if !ok {
		panic(fmt.Sprintf("unexpected PR review thread type: %T", id))
	}
	return thread
}

// mustPRReviewComment narrows a forge review comment ID to GitHub's ID type.
func mustPRReviewComment(id forge.ReviewCommentID) *PRReviewComment {
	if id == nil {
		return nil
	}
	comment, ok := id.(*PRReviewComment)
	if !ok {
		panic(fmt.Sprintf("unexpected PR review comment type: %T", id))
	}
	return comment
}

// reviewDisposition translates an effective GitHub review state.
func reviewDisposition(state github.ReviewState) (forge.ReviewDisposition, bool) {
	switch state {
	case github.ReviewStateApproved:
		return forge.ReviewDispositionApprove, true
	case github.ReviewStateChangesRequested:
		return forge.ReviewDispositionRequestChanges, true
	case github.ReviewStateCommented, github.ReviewStateDismissed, github.ReviewStatePending, github.ReviewStateUnknown:
		return 0, false
	default:
		return 0, false
	}
}

// reviewLine prefers a current line, then its original outdated location.
func reviewLine(current, original *int, fallback int) int {
	if current != nil && *current > 0 {
		return *current
	}
	if original != nil && *original > 0 {
		return *original
	}
	return fallback
}

// reviewThreadSide translates GitHub's diff side to the shared review model.
func reviewThreadSide(side github.DiffSide) forge.ReviewThreadSide {
	if side == github.DiffSideLeft {
		return forge.ReviewThreadSideLeft
	}
	return forge.ReviewThreadSideRight
}

// diffSide translates a shared review side to GitHub's diff side.
func diffSide(side forge.ReviewThreadSide) github.DiffSide {
	switch side {
	case forge.ReviewThreadSideRight:
		return github.DiffSideRight
	case forge.ReviewThreadSideLeft:
		return github.DiffSideLeft
	default:
		panic(fmt.Sprintf("unexpected review thread side: %v", side))
	}
}

// reviewEvent translates an effective review disposition to GitHub's event.
func reviewEvent(disposition forge.ReviewDisposition) github.ReviewEvent {
	switch disposition {
	case forge.ReviewDispositionApprove:
		return github.ReviewEventApprove
	case forge.ReviewDispositionRequestChanges:
		return github.ReviewEventRequestChanges
	default:
		panic(fmt.Sprintf("unexpected review disposition: %v", disposition))
	}
}
