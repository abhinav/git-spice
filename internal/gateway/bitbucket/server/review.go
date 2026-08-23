package server

import (
	"context"
	"fmt"
	"iter"
	"strconv"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/bitbucket"
	"go.abhg.dev/gs/internal/git"
	"golang.org/x/mod/semver"
)

// Review feature boundaries come from these Atlassian references:
//   - 7.7 native review workflow and completion endpoint:
//     https://docs.atlassian.com/bitbucket-server/rest/7.7.1/bitbucket-rest.html
//   - 8.9 resolvable comment threads:
//     https://confluence.atlassian.com/bitbucketserver089/enhancements-to-your-code-review-workflow-1236435900.html
//   - 9.2 multiline comments:
//     https://confluence.atlassian.com/display/BitbucketServer/Bitbucket%2BData%2BCenter%2B9.2%2Brelease%2Bnotes
const (
	_reviewMinVersion     = "7.7.0"
	_reviewMinBuildNumber = 7007000
	_resolutionMinVersion = "8.9.0"
	_resolutionMinBuild   = 8009000
	_multilineMinVersion  = "9.2.0"
	_multilineMinBuild    = 9002000
)

var (
	_ bitbucket.ReviewGateway        = (*Gateway)(nil)
	_ bitbucket.PendingReviewGateway = (*Gateway)(nil)
)

// ReviewCapabilities derives the Data Center review surface from the running
// product version. The API additions are versioned independently, so exposing
// ReviewRepository and ReviewThreadResolver are separate decisions.
func (g *Gateway) ReviewCapabilities(
	ctx context.Context,
) (bitbucket.ReviewCapabilities, error) {
	props, err := g.client.ApplicationProperties(ctx)
	if err != nil {
		return bitbucket.ReviewCapabilities{}, fmt.Errorf(
			"read Bitbucket Data Center version: %w", err)
	}

	version, build, err := applicationVersion(props)
	if err != nil {
		return bitbucket.ReviewCapabilities{}, err
	}
	capabilities := bitbucket.ReviewCapabilities{
		Supported: versionAtLeast(
			version, build, _reviewMinVersion, _reviewMinBuildNumber),
	}
	capabilities.NativeDrafts = capabilities.Supported
	capabilities.FileLevel = capabilities.Supported
	capabilities.ThreadResolution = versionAtLeast(
		version, build, _resolutionMinVersion, _resolutionMinBuild)
	capabilities.Multiline = versionAtLeast(
		version, build, _multilineMinVersion, _multilineMinBuild)
	return capabilities, nil
}

// applicationVersion normalizes the semantic version when available and falls
// back to Atlassian's numeric build encoding for customized version strings.
func applicationVersion(
	props *ApplicationProperties,
) (version string, build int, _ error) {
	if semver.IsValid("v" + props.Version) {
		return "v" + props.Version, 0, nil
	}

	build, err := strconv.Atoi(props.BuildNumber)
	if err == nil && build > 0 {
		return "", build, nil
	}
	return "", 0, fmt.Errorf(
		"unrecognized Bitbucket Data Center version %q (build %q)",
		props.Version, props.BuildNumber)
}

// versionAtLeast compares whichever representation applicationVersion found.
func versionAtLeast(
	version string,
	build int,
	minimumVersion string,
	minimumBuild int,
) bool {
	if version != "" {
		return semver.Compare(version, "v"+minimumVersion) >= 0
	}
	return build >= minimumBuild
}

// ReviewContext captures the pull request revisions and optimistic-locking
// version before any pending review comment is created.
func (g *Gateway) ReviewContext(
	ctx context.Context,
	prID int64,
) (bitbucket.ReviewContext, error) {
	pr, _, err := g.client.PullRequestGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID)
	if err != nil {
		return bitbucket.ReviewContext{}, fmt.Errorf("get pull request: %w", err)
	}
	if pr.ToRef.LatestCommit == "" || pr.FromRef.LatestCommit == "" {
		return bitbucket.ReviewContext{}, fmt.Errorf(
			"pull request %d did not report both review revisions", prID)
	}
	return bitbucket.ReviewContext{
		BaseHash: git.Hash(pr.ToRef.LatestCommit),
		HeadHash: git.Hash(pr.FromRef.LatestCommit),
		Version:  pr.Version,
	}, nil
}

// ReviewAnchor resolves Data Center's required line classifications from the
// structured diff before any pending review comment is created.
func (g *Gateway) ReviewAnchor(
	ctx context.Context,
	prID int64,
	review bitbucket.ReviewContext,
	path string,
	lines forge.ReviewThreadRange,
	side forge.ReviewThreadSide,
) (bitbucket.ReviewAnchor, error) {
	if lines.IsZero() {
		return bitbucket.ReviewAnchor{}, nil
	}
	diff, _, err := g.client.PullRequestDiff(
		ctx,
		g.repoID.restProjectKey(),
		g.repoID.slug,
		prID,
		path,
		string(review.BaseHash),
		string(review.HeadHash),
	)
	if err != nil {
		return bitbucket.ReviewAnchor{}, fmt.Errorf(
			"get pull request diff for %q: %w", path, err)
	}

	var anchor bitbucket.ReviewAnchor
	for _, hunk := range diff.Hunks {
		for _, segment := range hunk.Segments {
			for _, line := range segment.Lines {
				coordinate, eligible := reviewLineCoordinate(side, segment.Type, line)
				if !eligible {
					continue
				}
				if coordinate == lines.StartLine {
					anchor.StartLineType = segment.Type
				}
				if coordinate == lines.EndLine {
					anchor.EndLineType = segment.Type
				}
			}
		}
	}
	if anchor.StartLineType == "" || anchor.EndLineType == "" {
		return bitbucket.ReviewAnchor{}, fmt.Errorf(
			"review range %d-%d on the %s side of %q is not present in the pull request diff",
			lines.StartLine, lines.EndLine, side, path,
		)
	}
	return anchor, nil
}

// reviewLineCoordinate selects the coordinate and segment classifications that
// exist on the requested file side. Removed lines have no destination
// coordinate; added lines have no source coordinate.
func reviewLineCoordinate(
	side forge.ReviewThreadSide,
	lineType string,
	line DiffLine,
) (coordinate int, eligible bool) {
	switch side {
	case forge.ReviewThreadSideRight:
		if lineType != "ADDED" && lineType != "CONTEXT" {
			return 0, false
		}
		return line.Destination, true
	case forge.ReviewThreadSideLeft:
		if lineType != "REMOVED" && lineType != "CONTEXT" {
			return 0, false
		}
		return line.Source, true
	default:
		panic(fmt.Sprintf("bitbucket: invalid review thread side %d", side))
	}
}

// ListReviewerStates lists effective review dispositions for concrete revisions.
// Data Center does not expose the review submission timestamp on participants.
func (g *Gateway) ListReviewerStates(
	ctx context.Context,
	prID int64,
) iter.Seq2[*bitbucket.ReviewerState, error] {
	return func(yield func(*bitbucket.ReviewerState, error) bool) {
		pr, _, err := g.client.PullRequestGet(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, prID)
		if err != nil {
			yield(nil, fmt.Errorf("get pull request: %w", err))
			return
		}

		for _, reviewer := range pr.Reviewers {
			if reviewer.LastReviewedCommit == "" {
				continue
			}
			var disposition forge.ReviewDisposition
			switch reviewer.Status {
			case "APPROVED":
				disposition = forge.ReviewDispositionApprove
			case "NEEDS_WORK":
				disposition = forge.ReviewDispositionRequestChanges
			default:
				continue
			}
			if !yield(&bitbucket.ReviewerState{
				Reviewer:    reviewer.User.Name,
				Disposition: disposition,
				CommitHash:  git.Hash(reviewer.LastReviewedCommit),
			}, nil) {
				return
			}
		}
	}
}

// ListReviewThreads reconstructs comment-backed threads from the pull request
// comment listing. General comments and separately listed replies are omitted;
// replies nested under each anchored root are flattened in server order.
func (g *Gateway) ListReviewThreads(
	ctx context.Context,
	prID int64,
) iter.Seq2[*bitbucket.ReviewThread, error] {
	return func(yield func(*bitbucket.ReviewThread, error) bool) {
		for comment, err := range g.client.CommentList(
			ctx, g.repoID.restProjectKey(), g.repoID.slug, prID,
		) {
			if err != nil {
				yield(nil, fmt.Errorf("list pull request comments: %w", err))
				return
			}
			if comment.Parent != nil || comment.Anchor == nil {
				continue
			}
			thread, err := reviewThreadFromServerComment(&comment)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(thread, nil) {
				return
			}
		}
	}
}

// reviewThreadFromServerComment maps Data Center's fileType independently from
// lineType: fileType owns the forge side, while segment types describe the
// endpoints of the inclusive range.
func reviewThreadFromServerComment(
	comment *Comment,
) (*bitbucket.ReviewThread, error) {
	anchor := comment.Anchor
	if isGeneralFileAnchor(anchor) {
		thread := &bitbucket.ReviewThread{
			RootCommentID: comment.ID,
			Path:          reviewPath(anchor.Path),
			CommitHash:    git.Hash(anchor.ToHash),
			Resolved:      comment.ThreadResolved,
		}
		appendServerReviewComment(&thread.Comments, comment)
		return thread, nil
	}
	if anchor.Line <= 0 || anchor.LineType == "" {
		return nil, fmt.Errorf(
			"review comment %d has malformed line anchor", comment.ID)
	}
	start := anchor.Line
	if anchor.MultilineMarker != nil {
		if anchor.MultilineMarker.StartLine <= 0 ||
			anchor.MultilineMarker.StartLine > anchor.Line ||
			anchor.MultilineMarker.StartLineType == "" {
			return nil, fmt.Errorf(
				"review comment %d has malformed multiline anchor", comment.ID)
		}
		start = anchor.MultilineMarker.StartLine
	}
	var side forge.ReviewThreadSide
	switch anchor.FileType {
	case "TO":
		side = forge.ReviewThreadSideRight
	case "FROM":
		side = forge.ReviewThreadSideLeft
	default:
		return nil, fmt.Errorf(
			"review comment %d has unrecognized file side %q",
			comment.ID, anchor.FileType)
	}
	thread := &bitbucket.ReviewThread{
		RootCommentID: comment.ID,
		Path:          reviewPath(anchor.Path),
		CommitHash:    git.Hash(anchor.ToHash),
		Range: forge.ReviewThreadRange{
			StartLine: start,
			EndLine:   anchor.Line,
		},
		Side:     side,
		Resolved: comment.ThreadResolved,
	}
	appendServerReviewComment(&thread.Comments, comment)
	return thread, nil
}

// isGeneralFileAnchor recognizes Data Center's general file-comment shape.
// The API uses a RANGE anchor with both revisions, path, and srcPath, but no
// fileType or line fields. Requiring that complete shape keeps missing line data
// from being interpreted as a file-level thread.
//
// See https://developer.atlassian.com/server/bitbucket/rest/v904/api-group-pull-requests/
func isGeneralFileAnchor(anchor *CommentAnchor) bool {
	return anchor.DiffType == "RANGE" &&
		anchor.FileType == "" &&
		anchor.FromHash != "" &&
		anchor.Line == 0 &&
		anchor.LineType == "" &&
		anchor.MultilineMarker == nil &&
		reviewPath(anchor.Path) != "" &&
		reviewPath(anchor.SrcPath) != "" &&
		anchor.ToHash != ""
}

// appendServerReviewComment flattens Data Center's nested reply tree into the
// chronological root-first representation required by ReviewRepository.
func appendServerReviewComment(dst *[]bitbucket.ReviewComment, comment *Comment) {
	*dst = append(*dst, bitbucket.ReviewComment{
		ID:        comment.ID,
		Version:   comment.Version,
		Body:      comment.Text,
		Author:    comment.Author.Name,
		CreatedAt: reviewCommentTime(comment.CreatedDate),
	})
	for i := range comment.Comments {
		appendServerReviewComment(dst, &comment.Comments[i])
	}
}

// reviewCommentTime preserves a missing product timestamp as Go's zero time.
func reviewCommentTime(milliseconds int64) time.Time {
	if milliseconds == 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds)
}

// reviewPath normalizes both Data Center path object shapes decoded by RestPath.
func reviewPath(path RestPath) string {
	if len(path.Components) > 0 {
		return strings.Join(path.Components, "/")
	}
	if path.Parent != "" {
		return path.Parent + "/" + path.Name
	}
	return path.Name
}

// CreateReviewComment creates an unpublished pending comment. Root comments
// carry the exact base/head revision pair used to produce their diff anchor;
// replies carry only the parent comment ID.
func (g *Gateway) CreateReviewComment(
	ctx context.Context,
	prID int64,
	req bitbucket.CreateReviewCommentRequest,
) (*bitbucket.ReviewComment, error) {
	apiReq := ReviewCommentCreateRequest{
		Text:  req.Body,
		State: "PENDING",
	}
	if req.ParentID != 0 {
		apiReq.Parent = &CommentRef{ID: req.ParentID}
	} else {
		apiReq.Anchor = reviewCommentAnchor(req)
	}

	comment, _, err := g.client.ReviewCommentCreate(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID, apiReq)
	if err != nil {
		return nil, fmt.Errorf("create pending review comment: %w", err)
	}
	return &bitbucket.ReviewComment{
		ID:        comment.ID,
		Version:   comment.Version,
		Body:      comment.Text,
		Author:    comment.Author.Name,
		CreatedAt: reviewCommentTime(comment.CreatedDate),
	}, nil
}

// reviewCommentAnchor combines the preflighted diff classifications with the
// native file side and exact revision pair. It performs no remote lookup and is
// called only after all anchors in the review have been prepared.
func reviewCommentAnchor(
	req bitbucket.CreateReviewCommentRequest,
) *CommentAnchorCreate {
	anchor := &CommentAnchorCreate{
		DiffType: "RANGE",
		FromHash: string(req.ReviewContext.BaseHash),
		Path:     req.Path,
		ToHash:   string(req.ReviewContext.HeadHash),
	}
	if req.Range.IsZero() {
		anchor.SrcPath = req.Path
		return anchor
	}
	if req.ReviewAnchor.EndLineType == "" ||
		(req.Range.StartLine != req.Range.EndLine &&
			req.ReviewAnchor.StartLineType == "") {
		panic("bitbucket: review comment anchor was not classified")
	}
	anchor.Line = req.Range.EndLine
	anchor.LineType = req.ReviewAnchor.EndLineType
	switch req.Side {
	case forge.ReviewThreadSideRight:
		anchor.FileType = "TO"
	case forge.ReviewThreadSideLeft:
		anchor.FileType = "FROM"
	default:
		panic(fmt.Sprintf("bitbucket: invalid review thread side %d", req.Side))
	}
	if req.Range.StartLine != req.Range.EndLine {
		anchor.MultilineMarker = &MultilineMarker{
			StartLine:     req.Range.StartLine,
			StartLineType: req.ReviewAnchor.StartLineType,
		}
	}
	return anchor
}

// PublishReview finishes the native review, making all pending comments, the
// optional summary, and the optional disposition visible atomically. The pull
// request version supplies optimistic concurrency, and the completion endpoint
// owns participant status, so publication needs no separate participant lookup
// or update.
func (g *Gateway) PublishReview(
	ctx context.Context,
	prID int64,
	review bitbucket.ReviewContext,
	body string,
	disposition forge.ReviewDisposition,
) error {
	var participantStatus string
	switch disposition {
	case forge.ReviewDispositionNone:
	case forge.ReviewDispositionApprove:
		participantStatus = "approved"
	case forge.ReviewDispositionRequestChanges:
		participantStatus = "needs_work"
	default:
		panic(fmt.Sprintf("bitbucket: invalid review disposition %d", disposition))
	}
	_, err := g.client.PullRequestFinishReview(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID, review.Version,
		PullRequestFinishReviewRequest{
			CommentText:       body,
			ParticipantStatus: participantStatus,
		})
	if err != nil {
		return fmt.Errorf("publish review: %w", err)
	}
	return nil
}

// ResolveReviewThread resolves the thread rooted at commentID.
func (g *Gateway) ResolveReviewThread(
	ctx context.Context,
	prID int64,
	commentID int64,
) error {
	return g.setReviewThreadResolved(ctx, prID, commentID, true)
}

// UnresolveReviewThread reopens the thread rooted at commentID.
func (g *Gateway) UnresolveReviewThread(
	ctx context.Context,
	prID int64,
	commentID int64,
) error {
	return g.setReviewThreadResolved(ctx, prID, commentID, false)
}

// setReviewThreadResolved fetches the live comment first because Data Center
// owns the optimistic-locking version used by the resolution update.
func (g *Gateway) setReviewThreadResolved(
	ctx context.Context,
	prID int64,
	commentID int64,
	resolved bool,
) error {
	comment, _, err := g.client.CommentGet(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID, commentID)
	if err != nil {
		return fmt.Errorf("get review thread: %w", err)
	}
	_, _, err = g.client.CommentReviewUpdate(
		ctx, g.repoID.restProjectKey(), g.repoID.slug, prID, commentID,
		CommentReviewUpdateRequest{
			Text:           comment.Text,
			Version:        comment.Version,
			ThreadResolved: &resolved,
		})
	if err != nil {
		return fmt.Errorf("update review thread resolution: %w", err)
	}
	return nil
}
