package cloud

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

var (
	_ bitbucket.ReviewGateway         = (*Gateway)(nil)
	_ bitbucket.EmulatedReviewGateway = (*Gateway)(nil)
)

// ReviewCapabilities reports the review features available from Bitbucket
// Cloud. Unlike Data Center, Cloud does not need a product-version probe.
func (*Gateway) ReviewCapabilities(
	context.Context,
) (bitbucket.ReviewCapabilities, error) {
	return bitbucket.ReviewCapabilities{
		Supported:        true,
		Multiline:        true,
		ThreadResolution: true,
	}, nil
}

// ListReviewerStates lists participants with an effective review disposition.
// Bitbucket Cloud does not expose the commit reviewed by a participant.
func (g *Gateway) ListReviewerStates(
	ctx context.Context,
	prID int64,
) iter.Seq2[*bitbucket.ReviewerState, error] {
	return func(yield func(*bitbucket.ReviewerState, error) bool) {
		pr, err := g.getPullRequest(ctx, prID)
		if err != nil {
			yield(nil, err)
			return
		}

		for _, participant := range pr.Participants {
			if participant.ParticipatedOn == nil {
				continue
			}

			var disposition forge.ReviewDisposition
			switch strings.ToLower(participant.State) {
			case "approved":
				disposition = forge.ReviewDispositionApprove
			case "changes_requested":
				disposition = forge.ReviewDispositionRequestChanges
			default:
				if !participant.Approved {
					continue
				}
				disposition = forge.ReviewDispositionApprove
			}

			if !yield(&bitbucket.ReviewerState{
				Reviewer:    extractUsername(&participant.User),
				Disposition: disposition,
				SubmittedAt: *participant.ParticipatedOn,
			}, nil) {
				return
			}
		}
	}
}

// ListReviewThreads reconstructs comment-backed review threads.
// The comments endpoint returns roots and replies oldest first, so a reply can
// be attached to the previously observed root without another API request.
func (g *Gateway) ListReviewThreads(
	ctx context.Context,
	prID int64,
) iter.Seq2[*bitbucket.ReviewThread, error] {
	return func(yield func(*bitbucket.ReviewThread, error) bool) {
		var threads []*bitbucket.ReviewThread
		threadsByComment := make(map[int64]*bitbucket.ReviewThread)
		for comment, err := range g.listPullRequestComments(ctx, prID) {
			if err != nil {
				yield(nil, err)
				return
			}

			// Replies repeat the root's inline coordinates in Bitbucket payloads.
			// Parent must therefore take precedence over Inline classification.
			if comment.Parent != nil {
				thread := threadsByComment[comment.Parent.ID]
				if thread == nil {
					continue
				}
				thread.Comments = append(
					thread.Comments, reviewCommentFromAPI(comment))
				threadsByComment[comment.ID] = thread
				continue
			}
			if comment.Inline != nil {
				thread := reviewThreadFromComment(comment)
				threadsByComment[comment.ID] = thread
				threads = append(threads, thread)
			}
		}

		for _, thread := range threads {
			if !yield(thread, nil) {
				return
			}
		}
	}
}

// reviewThreadFromComment maps Cloud's to/start_to postimage coordinates or
// from/start_from preimage coordinates into one inclusive forge range.
func reviewThreadFromComment(comment *Comment) *bitbucket.ReviewThread {
	inline := comment.Inline
	side := forge.ReviewThreadSideRight
	start, end := inlineLineRange(inline.StartTo, inline.To)
	if inline.To == nil {
		side = forge.ReviewThreadSideLeft
		start, end = inlineLineRange(inline.StartFrom, inline.From)
	}
	return &bitbucket.ReviewThread{
		RootCommentID: comment.ID,
		Path:          inline.Path,
		Range: forge.ReviewThreadRange{
			StartLine: start,
			EndLine:   end,
		},
		Side:     side,
		Resolved: comment.Resolution != nil,
		Comments: []bitbucket.ReviewComment{reviewCommentFromAPI(comment)},
	}
}

// inlineLineRange treats an omitted start coordinate as a single-line anchor.
func inlineLineRange(start, end *int) (int, int) {
	if end == nil {
		return 0, 0
	}
	if start == nil {
		return *end, *end
	}
	return *start, *end
}

// reviewCommentFromAPI retains the product fields exposed by ReviewComment.
func reviewCommentFromAPI(comment *Comment) bitbucket.ReviewComment {
	return bitbucket.ReviewComment{
		ID:        comment.ID,
		Body:      comment.Content.Raw,
		Author:    extractUsername(&comment.User),
		CreatedAt: comment.CreatedOn,
	}
}

// CreateReviewComment creates an inline comment or replies to a root comment.
func (g *Gateway) CreateReviewComment(
	ctx context.Context,
	prID int64,
	req bitbucket.CreateReviewCommentRequest,
) (*bitbucket.ReviewComment, error) {
	apiReq := &CommentCreateRequest{Content: Content{Raw: req.Body}}
	if req.ParentID != 0 {
		apiReq.Parent = &CommentRef{ID: req.ParentID}
	} else {
		apiReq.Inline = mustReviewInline(req.Path, req.Range, req.Side)
	}

	comment, _, err := g.client.CommentCreate(
		ctx, g.workspace, g.repo, prID, apiReq,
	)
	if err != nil {
		return nil, fmt.Errorf("create review comment: %w", err)
	}
	result := reviewCommentFromAPI(comment)
	return &result, nil
}

// mustReviewInline converts a git-spice-owned location into Cloud's side-
// specific inline fields. Invalid coordinates are internal invariant failures.
func mustReviewInline(
	path string,
	lines forge.ReviewThreadRange,
	side forge.ReviewThreadSide,
) *Inline {
	if path == "" {
		panic("bitbucket: review comment path is empty")
	}
	if lines.StartLine <= 0 || lines.EndLine < lines.StartLine {
		panic(fmt.Sprintf("bitbucket: invalid review comment range: %d-%d",
			lines.StartLine, lines.EndLine))
	}

	inline := &Inline{Path: path}
	switch side {
	case forge.ReviewThreadSideRight:
		inline.To = &lines.EndLine
		if lines.StartLine != lines.EndLine {
			inline.StartTo = &lines.StartLine
		}
	case forge.ReviewThreadSideLeft:
		inline.From = &lines.EndLine
		if lines.StartLine != lines.EndLine {
			inline.StartFrom = &lines.StartLine
		}
	default:
		panic(fmt.Sprintf("bitbucket: invalid review comment side: %s", side))
	}
	return inline
}

// SetReviewDisposition applies a native Bitbucket Cloud review disposition.
func (g *Gateway) SetReviewDisposition(
	ctx context.Context,
	prID int64,
	disposition forge.ReviewDisposition,
) error {
	var err error
	switch disposition {
	case forge.ReviewDispositionApprove:
		_, err = g.client.PullRequestApprove(ctx, g.workspace, g.repo, prID)
	case forge.ReviewDispositionRequestChanges:
		_, err = g.client.PullRequestRequestChanges(ctx, g.workspace, g.repo, prID)
	default:
		panic(fmt.Sprintf("bitbucket: invalid review disposition %d", disposition))
	}
	if err != nil {
		return fmt.Errorf("set review disposition: %w", err)
	}
	return nil
}

// ResolveReviewThread resolves the thread rooted at commentID.
func (g *Gateway) ResolveReviewThread(
	ctx context.Context,
	prID int64,
	commentID int64,
) error {
	if _, err := g.client.CommentResolve(
		ctx, g.workspace, g.repo, prID, commentID,
	); err != nil {
		return fmt.Errorf("resolve review thread: %w", err)
	}
	return nil
}

// UnresolveReviewThread reopens the thread rooted at commentID.
func (g *Gateway) UnresolveReviewThread(
	ctx context.Context,
	prID int64,
	commentID int64,
) error {
	if _, err := g.client.CommentUnresolve(
		ctx, g.workspace, g.repo, prID, commentID,
	); err != nil {
		return fmt.Errorf("unresolve review thread: %w", err)
	}
	return nil
}
