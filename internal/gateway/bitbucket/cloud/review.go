package cloud

import (
	"context"
	"errors"
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
		FileLevel:        true,
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
				thread, err := reviewThreadFromComment(comment)
				if err != nil {
					yield(nil, err)
					return
				}
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

// reviewThreadFromComment maps Cloud's path-only file anchor or side-specific
// line coordinates into a forge review location.
func reviewThreadFromComment(comment *Comment) (*bitbucket.ReviewThread, error) {
	inline := comment.Inline
	if inline.Path == "" {
		return nil, fmt.Errorf(
			"review comment %d has malformed inline anchor: path is empty",
			comment.ID)
	}
	lines, side, err := reviewInlineLocation(inline)
	if err != nil {
		return nil, fmt.Errorf(
			"review comment %d has malformed inline anchor: %w", comment.ID, err)
	}
	return &bitbucket.ReviewThread{
		RootCommentID: comment.ID,
		Path:          inline.Path,
		Range:         lines,
		Side:          side,
		Resolved:      comment.Resolution != nil,
		Comments:      []bitbucket.ReviewComment{reviewCommentFromAPI(comment)},
	}, nil
}

// reviewInlineLocation recognizes Cloud's file-level comment shape.
// The pull request comment API uses a path-only inline anchor for a whole-file
// comment; any line coordinates select a side-specific line anchor.
//
// See https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/
func reviewInlineLocation(
	inline *Inline,
) (forge.ReviewThreadRange, forge.ReviewThreadSide, error) {
	if inline.From == nil && inline.To == nil &&
		inline.StartFrom == nil && inline.StartTo == nil {
		return forge.ReviewThreadRange{}, forge.ReviewThreadSideRight, nil
	}

	side := forge.ReviewThreadSideRight
	start, end := inline.StartTo, inline.To
	if end == nil {
		side = forge.ReviewThreadSideLeft
		start, end = inline.StartFrom, inline.From
	}
	if end == nil {
		return forge.ReviewThreadRange{}, 0, errors.New(
			"line start is present without an endpoint")
	}
	startLine := *end
	if start == nil {
		start = &startLine
	}
	if *start <= 0 || *end < *start {
		return forge.ReviewThreadRange{}, 0, fmt.Errorf(
			"invalid line range %d-%d", *start, *end)
	}
	return forge.ReviewThreadRange{
		StartLine: *start,
		EndLine:   *end,
	}, side, nil
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
	if lines.IsZero() {
		return &Inline{Path: path}
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
