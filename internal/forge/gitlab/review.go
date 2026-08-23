package gitlab

import (
	"context"
	"crypto/sha1" // #nosec G505 -- GitLab defines line codes with SHA-1.
	"errors"
	"fmt"
	"iter"
	"strconv"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/gitlab"
)

const (
	positionTypeFile = "file"
	positionTypeText = "text"
)

// GitLab review mapping
//
// GitLab stores a code-review thread as one merge request discussion whose
// notes are the root comment and ordered replies. ListReviewThreads maps each
// resolvable, positioned discussion to one ReviewThread and maps every note to
// one ReviewComment.
//
// GitLab scopes discussion operations to a merge request and note edits to a
// merge request plus discussion. MRDiscussion therefore retains the merge
// request and discussion IDs needed for replies and resolution, while
// MRReviewComment additionally retains the note ID needed for direct edits.
// These review IDs are distinct from MRComment, which identifies ordinary
// top-level merge request comments.
//
// New positioned discussions also require the merge request's base, start,
// and head diff refs. SubmitReview fetches those refs once before creating its
// first new thread. A zero shared range maps to GitLab's file position type,
// which has no line or side coordinates. For line ranges, the shared review
// API supplies one diff side, so changed-side ranges use zero for the absent
// old or new coordinate in GitLab's SHA1(path)_old_new line code. Context
// ranges require both real coordinates; they remain unsupported until the
// provider parses the diff to recover that mapping.

var (
	_ forge.ReviewRepository     = (*Repository)(nil)
	_ forge.ReviewCommentEditor  = (*Repository)(nil)
	_ forge.ReviewThreadResolver = (*Repository)(nil)
)

// MRDiscussion identifies one GitLab merge request discussion.
//
// GitLab's reply and resolution endpoints scope an opaque discussion ID to a
// merge request. String therefore appends the merge request number to the
// discussion ID as "discussionID:mrNumber". The final colon is the separator,
// so colons in GitLab's opaque discussion ID remain unambiguous.
type MRDiscussion struct {
	// DiscussionID is GitLab's opaque discussion identifier.
	DiscussionID string

	// MRNumber is the merge request that scopes DiscussionID.
	MRNumber int64
}

// String reports the stable forge-facing spelling of the discussion ID.
func (d *MRDiscussion) String() string {
	return d.DiscussionID + ":" + strconv.FormatInt(d.MRNumber, 10)
}

// MRReviewComment identifies one note in a GitLab review discussion.
// Its spelling is "discussionID:noteID:mrNumber"; the final two colons are
// separators, so colons in GitLab's opaque discussion ID remain unambiguous.
type MRReviewComment struct {
	// DiscussionID is the discussion that contains the note.
	DiscussionID string

	// NoteID is GitLab's numeric note identifier.
	NoteID int64

	// MRNumber is the merge request that scopes DiscussionID and NoteID.
	MRNumber int64
}

// String reports the stable forge-facing spelling of the review comment ID.
func (c *MRReviewComment) String() string {
	return c.DiscussionID + ":" +
		strconv.FormatInt(c.NoteID, 10) + ":" +
		strconv.FormatInt(c.MRNumber, 10)
}

// ListReviewerStates yields effective dispositions from assigned GitLab reviewers.
func (r *Repository) ListReviewerStates(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewerState, error] {
	mrNumber := mustMR(id).Number
	return func(yield func(*forge.ReviewerState, error) bool) {
		reviewers, _, err := r.client.MergeRequestReviewerList(
			ctx,
			r.repoID,
			mrNumber,
		)
		if err != nil {
			yield(nil, fmt.Errorf("list reviewers: %w", err))
			return
		}

		for _, reviewer := range reviewers {
			disposition, completed, err := reviewDisposition(reviewer.State)
			if err != nil {
				yield(nil, err)
				return
			}
			if !completed {
				continue
			}

			if !yield(&forge.ReviewerState{
				Reviewer:    reviewer.User.Username,
				Disposition: disposition,
			}, nil) {
				return
			}
		}
	}
}

// ListReviewThreads yields resolvable GitLab merge request discussions.
func (r *Repository) ListReviewThreads(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewThread, error] {
	mrNumber := mustMR(id).Number
	return func(yield func(*forge.ReviewThread, error) bool) {
		opts := gitlab.ListMergeRequestDiscussionsOptions{
			PerPage: 100,
		}
		for page := 1; ; page++ {
			discussions, response, err := r.client.MergeRequestDiscussionList(
				ctx,
				r.repoID,
				mrNumber,
				&opts,
			)
			if err != nil {
				yield(nil, fmt.Errorf("list discussions (page %d): %w", page, err))
				return
			}

			for _, discussion := range discussions {
				thread, ok, err := reviewThread(mrNumber, discussion)
				if err != nil {
					yield(nil, err)
					return
				}
				if !ok {
					continue
				}
				if !yield(thread, nil) {
					return
				}
			}

			if response.NextPage == 0 {
				return
			}
			opts.Page = int64(response.NextPage)
		}
	}
}

// SubmitReview publishes review comments in request order, then applies the
// GitLab-supported summary and disposition fallback.
// GitLab discussions publish individually,
// so an error may leave earlier comments visible
// even though the result is empty.
func (r *Repository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	// Reject internal enum bugs before GitLab can publish part of the submission.
	mustReviewDisposition(req.Disposition)
	mr := mustMR(id)
	result := forge.SubmitReviewResult{
		Comments: make([]forge.SubmitReviewCommentResult, 0, len(req.Comments)),
	}

	var diffRefs *gitlab.MergeRequestDiffRefs
	for _, comment := range req.Comments {
		if comment.ReplyTo == nil {
			var err error
			diffRefs, err = r.diffRefs(ctx, mr.Number)
			if err != nil {
				return forge.SubmitReviewResult{}, err
			}
			break
		}
	}

	for _, comment := range req.Comments {
		commentResult, err := r.submitReviewComment(
			ctx,
			mr.Number,
			comment,
			diffRefs,
		)
		if err != nil {
			return forge.SubmitReviewResult{}, err
		}
		result.Comments = append(result.Comments, commentResult)
	}

	if err := r.publishReviewDisposition(ctx, id, mr.Number, req); err != nil {
		return forge.SubmitReviewResult{}, err
	}
	r.log.Debug("Submitted change feedback",
		"mr", mr.Number,
		"comments", len(result.Comments),
		"disposition", req.Disposition,
	)
	return result, nil
}

// UpdateReviewComment replaces the body of a GitLab discussion note.
func (r *Repository) UpdateReviewComment(
	ctx context.Context,
	id forge.ReviewCommentID,
	body string,
) error {
	comment := mustMRReviewComment(id)

	_, _, err := r.client.MergeRequestDiscussionNoteUpdate(
		ctx,
		r.repoID,
		comment.MRNumber,
		comment.DiscussionID,
		comment.NoteID,
		&gitlab.UpdateMergeRequestDiscussionNoteOptions{Body: &body},
	)
	if err != nil {
		return fmt.Errorf("update review comment: %w", err)
	}
	return nil
}

// ResolveReviewThread marks a GitLab discussion resolved.
func (r *Repository) ResolveReviewThread(
	ctx context.Context,
	id forge.ReviewThreadID,
) error {
	return r.setReviewThreadResolved(ctx, mustMRDiscussion(id), true)
}

// UnresolveReviewThread reopens a resolved GitLab discussion.
func (r *Repository) UnresolveReviewThread(
	ctx context.Context,
	id forge.ReviewThreadID,
) error {
	return r.setReviewThreadResolved(ctx, mustMRDiscussion(id), false)
}

// reviewDisposition maps only GitLab states that express an effective
// disposition.
func reviewDisposition(
	state gitlab.ReviewerState,
) (forge.ReviewDisposition, bool, error) {
	switch state {
	case gitlab.ReviewerStateUnreviewed,
		gitlab.ReviewerStateReviewStarted,
		gitlab.ReviewerStateReviewed,
		gitlab.ReviewerStateUnapproved:
		return forge.ReviewDispositionNone, false, nil
	case gitlab.ReviewerStateRequestedChanges:
		return forge.ReviewDispositionRequestChanges, true, nil
	case gitlab.ReviewerStateApproved:
		return forge.ReviewDispositionApprove, true, nil
	default:
		return forge.ReviewDispositionNone, false,
			fmt.Errorf("unknown GitLab reviewer state %q", state)
	}
}

// reviewThread translates one positioned, resolvable discussion.
// Non-review discussions and empty provider records are omitted.
func reviewThread(
	mrNumber int64,
	discussion *gitlab.Discussion,
) (*forge.ReviewThread, bool, error) {
	if discussion == nil || len(discussion.Notes) == 0 {
		return nil, false, nil
	}
	root := discussion.Notes[0]
	if root == nil || !root.Resolvable || root.Position == nil {
		return nil, false, nil
	}

	path, lines, side, err := reviewThreadPosition(root.Position)
	if err != nil {
		return nil, false, fmt.Errorf(
			"translate discussion %q position: %w",
			discussion.ID,
			err,
		)
	}
	resolved := root.Resolved
	thread := &forge.ReviewThread{
		ID: &MRDiscussion{
			DiscussionID: discussion.ID,
			MRNumber:     mrNumber,
		},
		Path:     path,
		Range:    lines,
		Side:     side,
		Resolved: &resolved,
	}
	for _, note := range discussion.Notes {
		if note == nil {
			continue
		}
		createdAt := time.Time{}
		if note.CreatedAt != nil {
			createdAt = *note.CreatedAt
		}
		thread.Comments = append(thread.Comments, forge.ReviewComment{
			ID: &MRReviewComment{
				DiscussionID: discussion.ID,
				NoteID:       note.ID,
				MRNumber:     mrNumber,
			},
			Body:      note.Body,
			Author:    note.Author.Username,
			CreatedAt: createdAt,
		})
	}
	return thread, true, nil
}

// reviewThreadPosition decodes GitLab's position type before interpreting its
// type-specific coordinates. Text ranges use GitLab's selected endpoint type
// as the forge-neutral side and never substitute an opposite-side coordinate.
func reviewThreadPosition(
	position *gitlab.DiscussionPosition,
) (string, forge.ReviewThreadRange, forge.ReviewThreadSide, error) {
	switch position.PositionType {
	case positionTypeFile:
		return position.NewPath, forge.ReviewThreadRange{},
			forge.ReviewThreadSideRight, nil
	case positionTypeText:
	default:
		return "", forge.ReviewThreadRange{}, forge.ReviewThreadSideRight,
			fmt.Errorf("unknown position type %q", position.PositionType)
	}

	if position.LineRange != nil {
		start, end := position.LineRange.Start, position.LineRange.End
		if start.Type != end.Type {
			return "", forge.ReviewThreadRange{}, forge.ReviewThreadSideRight,
				fmt.Errorf("range crosses diff sides %q and %q", start.Type, end.Type)
		}
		switch start.Type {
		case "new":
			return position.NewPath, forge.ReviewThreadRange{
				StartLine: int(start.NewLine),
				EndLine:   int(end.NewLine),
			}, forge.ReviewThreadSideRight, nil
		case "old":
			return position.OldPath, forge.ReviewThreadRange{
				StartLine: int(start.OldLine),
				EndLine:   int(end.OldLine),
			}, forge.ReviewThreadSideLeft, nil
		default:
			return "", forge.ReviewThreadRange{}, forge.ReviewThreadSideRight,
				fmt.Errorf("unknown range side %q", start.Type)
		}
	}

	if position.NewLine != 0 {
		return position.NewPath,
			forge.ReviewThreadLine(int(position.NewLine)),
			forge.ReviewThreadSideRight,
			nil
	}
	if position.OldLine != 0 {
		return position.OldPath,
			forge.ReviewThreadLine(int(position.OldLine)),
			forge.ReviewThreadSideLeft,
			nil
	}
	return "", forge.ReviewThreadRange{}, forge.ReviewThreadSideRight,
		errors.New("position has no line")
}

// submitReviewComment publishes one request as either a new discussion or a
// note in an existing discussion and returns the complete scoped identifiers.
func (r *Repository) submitReviewComment(
	ctx context.Context,
	mrNumber int64,
	req forge.SubmitReviewCommentRequest,
	refs *gitlab.MergeRequestDiffRefs,
) (forge.SubmitReviewCommentResult, error) {
	if req.ReplyTo != nil {
		thread := mustMRDiscussion(req.ReplyTo)
		if thread.MRNumber != mrNumber {
			panic(fmt.Sprintf(
				"gitlab: review thread belongs to MR !%d, not !%d",
				thread.MRNumber,
				mrNumber,
			))
		}
		note, _, err := r.client.MergeRequestDiscussionNoteCreate(
			ctx,
			r.repoID,
			mrNumber,
			thread.DiscussionID,
			&gitlab.AddMergeRequestDiscussionNoteOptions{Body: &req.Body},
		)
		if err != nil {
			return forge.SubmitReviewCommentResult{}, fmt.Errorf(
				"reply to thread %s: %w",
				thread,
				err,
			)
		}
		return forge.SubmitReviewCommentResult{
			ThreadID: thread,
			CommentID: &MRReviewComment{
				DiscussionID: thread.DiscussionID,
				NoteID:       note.ID,
				MRNumber:     mrNumber,
			},
		}, nil
	}

	discussion, _, err := r.client.MergeRequestDiscussionCreate(
		ctx,
		r.repoID,
		mrNumber,
		newDiscussionOptions(req, refs),
	)
	location := req.Path
	if !req.Range.IsZero() {
		location = fmt.Sprintf("%s:%d", req.Path, req.Range.StartLine)
	}
	if err != nil {
		if req.Range.StartLine != req.Range.EndLine {
			return forge.SubmitReviewCommentResult{}, fmt.Errorf(
				"create discussion on %s: %w; "+
					"GitLab context ranges require both old and new "+
					"coordinates, which ReviewRepository does not expose",
				location,
				err,
			)
		}
		return forge.SubmitReviewCommentResult{}, fmt.Errorf(
			"create discussion on %s: %w",
			location,
			err,
		)
	}
	if len(discussion.Notes) == 0 {
		return forge.SubmitReviewCommentResult{}, fmt.Errorf(
			"create discussion on %s: response has no root note",
			location,
		)
	}

	return forge.SubmitReviewCommentResult{
		ThreadID: &MRDiscussion{
			DiscussionID: discussion.ID,
			MRNumber:     mrNumber,
		},
		CommentID: &MRReviewComment{
			DiscussionID: discussion.ID,
			NoteID:       discussion.Notes[0].ID,
			MRNumber:     mrNumber,
		},
	}, nil
}

// newDiscussionOptions maps a zero range to GitLab's coordinate-free file
// position. Text positions anchor at the inclusive range end and add
// line-range endpoints when the request spans lines.
func newDiscussionOptions(
	req forge.SubmitReviewCommentRequest,
	refs *gitlab.MergeRequestDiffRefs,
) *gitlab.CreateMergeRequestDiscussionOptions {
	positionType := positionTypeFile
	if !req.Range.IsZero() {
		positionType = positionTypeText
	}
	position := &gitlab.PositionOptions{
		BaseSHA:      &refs.BaseSHA,
		HeadSHA:      &refs.HeadSHA,
		StartSHA:     &refs.StartSHA,
		PositionType: &positionType,
		NewPath:      &req.Path,
		OldPath:      &req.Path,
	}
	if req.Range.IsZero() {
		return &gitlab.CreateMergeRequestDiscussionOptions{
			Body:     &req.Body,
			Position: position,
		}
	}

	start := int64(req.Range.StartLine)
	end := int64(req.Range.EndLine)
	switch req.Side {
	case forge.ReviewThreadSideLeft:
		position.OldLine = &end
	case forge.ReviewThreadSideRight:
		position.NewLine = &end
	default:
		panic(fmt.Sprintf("gitlab: unsupported review thread side %d", req.Side))
	}
	if start != end {
		position.LineRange = &gitlab.LineRangeOptions{
			Start: linePositionOptions(req.Path, req.Side, start),
			End:   linePositionOptions(req.Path, req.Side, end),
		}
	}

	return &gitlab.CreateMergeRequestDiscussionOptions{
		Body:     &req.Body,
		Position: position,
	}
}

// linePositionOptions constructs GitLab's SHA1(path)_old_new line code for a
// changed-side range. The coordinate absent from the selected side remains
// zero; guessing it would silently move context comments to the wrong line.
func linePositionOptions(
	path string,
	side forge.ReviewThreadSide,
	line int64,
) *gitlab.LinePositionOptions {
	position := &gitlab.LinePositionOptions{}
	var oldLine, newLine int64
	switch side {
	case forge.ReviewThreadSideLeft:
		kind := "old"
		oldLine = line
		position.Type = &kind
		position.OldLine = &oldLine
	case forge.ReviewThreadSideRight:
		kind := "new"
		newLine = line
		position.Type = &kind
		position.NewLine = &newLine
	default:
		panic(fmt.Sprintf("gitlab: unsupported review thread side %d", side))
	}
	lineCode := fmt.Sprintf("%x_%d_%d", sha1.Sum([]byte(path)), oldLine, newLine)
	position.LineCode = &lineCode
	return position
}

// diffRefs loads the three revisions GitLab requires to anchor a new thread.
func (r *Repository) diffRefs(
	ctx context.Context,
	mrNumber int64,
) (*gitlab.MergeRequestDiffRefs, error) {
	mr, _, err := r.client.MergeRequestGet(ctx, r.repoID, mrNumber, nil)
	if err != nil {
		return nil, fmt.Errorf("get merge request diff refs: %w", err)
	}
	return &mr.DiffRefs, nil
}

// mustReviewDisposition enforces the review dispositions supported by GitLab.
func mustReviewDisposition(disposition forge.ReviewDisposition) {
	switch disposition {
	case forge.ReviewDispositionNone,
		forge.ReviewDispositionApprove,
		forge.ReviewDispositionRequestChanges:
	default:
		panic(fmt.Sprintf(
			"gitlab: unsupported review disposition %d",
			disposition,
		))
	}
}

// publishReviewDisposition posts the submission body and applies the optional
// disposition outside GitLab discussions. It posts only the requested body or
// the provider-owned change-request fallback, then uses native approval for an
// approving review. It deliberately does not publish ambient draft notes.
func (r *Repository) publishReviewDisposition(
	ctx context.Context,
	id forge.ChangeID,
	mrNumber int64,
	req forge.SubmitReviewRequest,
) error {
	body := req.Body
	if body == "" && req.Disposition == forge.ReviewDispositionRequestChanges {
		body = "Changes requested."
	}
	if body != "" {
		if _, err := r.PostChangeComment(ctx, id, body); err != nil {
			return fmt.Errorf("post submission body: %w", err)
		}
	}

	if req.Disposition == forge.ReviewDispositionApprove {
		if _, err := r.client.MergeRequestApprove(
			ctx,
			r.repoID,
			mrNumber,
			&gitlab.ApproveMergeRequestOptions{},
		); err != nil {
			return fmt.Errorf("approve merge request: %w", err)
		}
	}
	return nil
}

// setReviewThreadResolved applies one resolution state through the
// merge-request-scoped discussion endpoint.
func (r *Repository) setReviewThreadResolved(
	ctx context.Context,
	thread *MRDiscussion,
	resolved bool,
) error {
	_, _, err := r.client.MergeRequestDiscussionResolve(
		ctx,
		r.repoID,
		thread.MRNumber,
		thread.DiscussionID,
		&gitlab.ResolveMergeRequestDiscussionOptions{Resolved: &resolved},
	)
	if err != nil {
		return fmt.Errorf("set review thread resolved to %t: %w", resolved, err)
	}
	return nil
}

// mustMRDiscussion converts a forge review-thread ID to its GitLab identity.
func mustMRDiscussion(id forge.ReviewThreadID) *MRDiscussion {
	thread, ok := id.(*MRDiscussion)
	if !ok {
		panic(fmt.Sprintf("gitlab: expected *MRDiscussion, got %T", id))
	}
	if thread == nil {
		panic("gitlab: nil *MRDiscussion")
	}
	return thread
}

// mustMRReviewComment converts a forge review-comment ID to its GitLab
// identity.
func mustMRReviewComment(
	id forge.ReviewCommentID,
) *MRReviewComment {
	comment, ok := id.(*MRReviewComment)
	if !ok {
		panic(fmt.Sprintf("gitlab: expected *MRReviewComment, got %T", id))
	}
	if comment == nil {
		panic("gitlab: nil *MRReviewComment")
	}
	return comment
}
