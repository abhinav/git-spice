package shamhub

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strconv"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/reviewdiff"
)

// ShamHub represents review threads in the same flat comment store as ordinary
// change comments. A thread ID is derived from its root comment ID, and replies
// carry that thread ID without repeating the root's diff coordinates.
//
// forgeRepository receives typed requests constructed inside git-spice and
// relies on the forge package's request invariants. The HTTP handler separately
// validates its decoded request because ShamHub is also an independently
// callable protocol boundary for tests.
var (
	_ forge.ReviewRepository     = (*forgeRepository)(nil)
	_ forge.ReviewCommentEditor  = (*forgeRepository)(nil)
	_ forge.ReviewThreadResolver = (*forgeRepository)(nil)
)

var (
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/reviews",
		(*ShamHub).handleSubmitReview,
	)
	_ = shamhubRESTHandler(
		"GET /{owner}/{repo}/reviews",
		(*ShamHub).handleListReviewerStates,
	)
	_ = shamhubRESTHandler(
		"GET /{owner}/{repo}/review-threads",
		(*ShamHub).handleListReviewThreads,
	)
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/threads/{threadID}/resolve",
		(*ShamHub).handleResolveReviewThread,
	)
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/threads/{threadID}/unresolve",
		(*ShamHub).handleUnresolveReviewThread,
	)
)

// ReviewThreadID identifies a ShamHub review thread.
type ReviewThreadID string

var _ forge.ReviewThreadID = ReviewThreadID("")

// String returns the thread ID used by ShamHub's HTTP protocol.
func (id ReviewThreadID) String() string {
	return string(id)
}

// mustReviewThreadID unwraps an ID produced by this adapter.
// An ID from another provider violates the shared forge API invariant.
func mustReviewThreadID(id forge.ReviewThreadID) ReviewThreadID {
	threadID, ok := id.(ReviewThreadID)
	if !ok {
		panic(fmt.Sprintf("shamhub: expected ReviewThreadID, got %T", id))
	}
	return threadID
}

// ReviewCommentID uniquely identifies a review comment in ShamHub.
type ReviewCommentID int

var _ forge.ReviewCommentID = ReviewCommentID(0)

// String returns the decimal comment ID used by ShamHub's HTTP protocol.
func (id ReviewCommentID) String() string {
	return strconv.Itoa(int(id))
}

// mustReviewCommentID unwraps an ID produced by this adapter.
// An ID from another provider violates the shared forge API invariant.
func mustReviewCommentID(id forge.ReviewCommentID) ReviewCommentID {
	commentID, ok := id.(ReviewCommentID)
	if !ok {
		panic(fmt.Sprintf("shamhub: expected ReviewCommentID, got %T", id))
	}
	return commentID
}

// SubmitReview translates a typed review into ShamHub's flat HTTP representation.
// Roots carry diff coordinates; replies carry only their thread ID and body.
func (r *forgeRepository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.SubmitReviewRequest,
) (forge.SubmitReviewResult, error) {
	comments := make([]submitReviewCommentRequest, len(req.Comments))
	for i, comment := range req.Comments {
		comments[i] = submitReviewCommentRequest{
			Body: comment.Body,
		}
		if comment.ReplyTo != nil {
			comments[i].ThreadID = mustReviewThreadID(comment.ReplyTo).String()
			continue
		}
		comments[i].Path = comment.Path
		if comment.Range.IsZero() {
			continue
		}
		comments[i].Line = comment.Range.StartLine
		comments[i].Side = mustReviewThreadSide(comment.Side)
		if comment.Range.EndLine != comment.Range.StartLine {
			comments[i].RangeStart = comment.Range.StartLine
			comments[i].RangeEnd = comment.Range.EndLine
		}
	}

	u := r.apiURL.JoinPath(r.owner, r.repo, "reviews")
	request := submitReviewRequest{
		Change:      int(id.(ChangeID)),
		Body:        req.Body,
		Disposition: mustReviewDisposition(req.Disposition),
		Comments:    comments,
	}

	var response submitReviewResponse
	if err := r.client.Post(ctx, u.String(), request, &response); err != nil {
		return forge.SubmitReviewResult{}, fmt.Errorf("submit review: %w", err)
	}

	result := forge.SubmitReviewResult{
		Comments: make([]forge.SubmitReviewCommentResult, len(response.Comments)),
	}
	for i, comment := range response.Comments {
		result.Comments[i] = forge.SubmitReviewCommentResult{
			ThreadID:  ReviewThreadID(comment.ThreadID),
			CommentID: ReviewCommentID(comment.CommentID),
		}
	}
	return result, nil
}

// mustReviewDisposition converts the closed disposition set to its wire value.
// An unknown value violates the shared forge API invariant.
func mustReviewDisposition(disposition forge.ReviewDisposition) int {
	switch disposition {
	case forge.ReviewDispositionNone,
		forge.ReviewDispositionApprove,
		forge.ReviewDispositionRequestChanges:
		return int(disposition)
	default:
		panic(fmt.Sprintf("shamhub: unsupported review disposition %d", disposition))
	}
}

// mustReviewThreadSide converts a root comment's diff side to its wire value.
// An unknown value violates the shared forge API invariant.
func mustReviewThreadSide(side forge.ReviewThreadSide) int {
	switch side {
	case forge.ReviewThreadSideRight, forge.ReviewThreadSideLeft:
		return int(side)
	default:
		panic(fmt.Sprintf("shamhub: unsupported review thread side %d", side))
	}
}

// validateReviewDisposition rejects unknown values decoded at the HTTP boundary.
func validateReviewDisposition(disposition forge.ReviewDisposition) error {
	switch disposition {
	case forge.ReviewDispositionNone,
		forge.ReviewDispositionApprove,
		forge.ReviewDispositionRequestChanges:
		return nil
	default:
		return fmt.Errorf("invalid review disposition: %d", disposition)
	}
}

// validateReviewRange enforces the protocol's one-based inclusive line range.
func validateReviewRange(reviewRange forge.ReviewThreadRange) error {
	if reviewRange.StartLine <= 0 || reviewRange.StartLine > reviewRange.EndLine {
		return fmt.Errorf(
			"invalid review range: start %d, end %d",
			reviewRange.StartLine,
			reviewRange.EndLine,
		)
	}
	return nil
}

// validateReviewSide rejects unknown values decoded at the HTTP boundary.
func validateReviewSide(side forge.ReviewThreadSide) error {
	switch side {
	case forge.ReviewThreadSideRight, forge.ReviewThreadSideLeft:
		return nil
	default:
		return fmt.Errorf("invalid review side: %d", side)
	}
}

// submitReviewRequest is untrusted path and JSON input until the HTTP handler
// validates it.
type submitReviewRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Change      int                          `json:"change,omitzero"`
	Body        string                       `json:"body,omitzero"`
	Disposition int                          `json:"disposition,omitzero"`
	Comments    []submitReviewCommentRequest `json:"comments,omitempty"`
}

// submitReviewCommentRequest carries either root coordinates or a reply thread ID.
// Single-line roots use Line; multi-line roots also populate the range fields.
type submitReviewCommentRequest struct {
	Path       string `json:"path,omitzero"`
	Line       int    `json:"line,omitzero"`
	RangeStart int    `json:"rangeStart,omitzero"`
	RangeEnd   int    `json:"rangeEnd,omitzero"`
	Side       int    `json:"side,omitzero"`
	ThreadID   string `json:"threadID,omitzero"`
	Body       string `json:"body,omitzero"`
}

type submitReviewResponse struct {
	Comments []submitReviewCommentResponse `json:"comments,omitempty"`
}

type submitReviewCommentResponse struct {
	ThreadID  string `json:"threadID"`
	CommentID int    `json:"commentID"`
}

// handleSubmitReview validates the HTTP request and atomically records its
// feedback submission with any resulting comments or threads.
func (sh *ShamHub) handleSubmitReview(
	ctx context.Context,
	req *submitReviewRequest,
) (*submitReviewResponse, error) {
	submitter, err := shamHubUserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateSubmitReviewRequest(req); err != nil {
		return nil, badRequestErrorf("%v", err)
	}

	rootCommitHash, err := sh.reviewRootCommitHash(
		ctx,
		req.Owner,
		req.Repo,
		req.Change,
		submitReviewCreatesRoot(req),
	)
	if err != nil {
		return nil, err
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()
	if !sh.changeBelongsToRepository(req.Change, req.Owner, req.Repo) {
		return nil, notFoundErrorf(
			"change %d not found in %s/%s",
			req.Change,
			req.Owner,
			req.Repo,
		)
	}
	if err := sh.validateSubmitReviewThreadIdentities(req); err != nil {
		return nil, badRequestErrorf("%v", err)
	}

	return sh.submitReview(submitter, req, rootCommitHash, time.Now())
}

// reviewRootCommitHash captures the change head selected when root creation
// begins. That revision remains the thread's identity if the branch later moves.
func (sh *ShamHub) reviewRootCommitHash(
	ctx context.Context,
	owner string,
	repo string,
	changeNumber int,
	createsRoot bool,
) (git.Hash, error) {
	if !createsRoot {
		return "", nil
	}

	head, err := sh.resolveReviewHead(ctx, owner, repo, changeNumber)
	if err != nil {
		return "", err
	}
	return head.Hash, nil
}

func submitReviewCreatesRoot(req *submitReviewRequest) bool {
	for _, comment := range req.Comments {
		if comment.ThreadID == "" {
			return true
		}
	}
	return false
}

type reviewHeadSnapshot struct {
	Hash   git.Hash
	Owner  string
	Repo   string
	Branch string
}

// resolveReviewHead snapshots and resolves the current head of a change. Git
// commands run after releasing the store lock because they may block.
func (sh *ShamHub) resolveReviewHead(
	ctx context.Context,
	owner string,
	repo string,
	changeNumber int,
) (reviewHeadSnapshot, error) {
	sh.mu.RLock()
	var head reviewHeadSnapshot
	found := false
	for _, change := range sh.changes {
		if change.Number != changeNumber ||
			change.Base.Owner != owner ||
			change.Base.Repo != repo {
			continue
		}
		found = true
		if change.Head != nil {
			head.Owner = change.Head.Owner
			head.Repo = change.Head.Repo
			head.Branch = change.Head.Name
		}
		if change.HeadHash != "" {
			head.Hash = git.Hash(change.HeadHash)
		}
		break
	}
	sh.mu.RUnlock()

	if !found {
		return reviewHeadSnapshot{}, notFoundErrorf(
			"change %d not found in %s/%s",
			changeNumber,
			owner,
			repo,
		)
	}
	if head.Hash != "" {
		return head, nil
	}
	if head.Branch == "" {
		return reviewHeadSnapshot{}, errors.New("change head is missing")
	}
	out, err := sh.gitCmd(
		ctx,
		head.Owner,
		head.Repo,
		"rev-parse",
		head.Branch,
	).Output()
	if err != nil {
		return reviewHeadSnapshot{}, fmt.Errorf("resolve change head: %w", err)
	}
	head.Hash = git.Hash(strings.TrimSpace(string(out)))
	return head, nil
}

// submitReview records one validated feedback submission while the caller
// holds sh.mu.
func (sh *ShamHub) submitReview(
	submitter string,
	req *submitReviewRequest,
	rootCommitHash git.Hash,
	now time.Time,
) (*submitReviewResponse, error) {
	commentIDs := make([]int, 0, len(req.Comments)+1)
	if req.Body != "" {
		// ShamHub represents a top-level review body as an ordinary change comment.
		commentID := sh.nextCommentID()
		sh.comments = append(sh.comments, shamComment{
			ID:        commentID,
			Change:    req.Change,
			Body:      req.Body,
			Author:    submitter,
			CreatedAt: now,
		})
		commentIDs = append(commentIDs, commentID)
	}

	response := &submitReviewResponse{
		Comments: make([]submitReviewCommentResponse, 0, len(req.Comments)),
	}
	for _, requestComment := range req.Comments {
		threadID := ReviewThreadID(requestComment.ThreadID)
		comment := shamComment{
			Change:    req.Change,
			Body:      requestComment.Body,
			Author:    submitter,
			CreatedAt: now,
		}
		if requestComment.ThreadID == "" {
			comment.Path = requestComment.Path
			if !reviewRangeFromRequest(requestComment).IsZero() {
				comment.Line = requestComment.Line
				comment.RangeStart = requestComment.RangeStart
				comment.RangeEnd = requestComment.RangeEnd
				comment.Side = forge.ReviewThreadSide(requestComment.Side)
			}
		}
		comment = sh.storeReviewComment(comment, threadID, rootCommitHash)
		response.Comments = append(response.Comments, submitReviewCommentResponse{
			ThreadID:  comment.ThreadID.String(),
			CommentID: comment.ID,
		})
		commentIDs = append(commentIDs, comment.ID)
	}

	sh.feedbackSubmissions = append(sh.feedbackSubmissions, shamFeedbackSubmission{
		Change:      req.Change,
		Submitter:   submitter,
		Disposition: forge.ReviewDisposition(req.Disposition),
		Body:        req.Body,
		CommentIDs:  commentIDs,
		SubmittedAt: now,
	})
	return response, nil
}

// storeReviewComment records a validated root or reply while the caller holds
// sh.mu. Roots own the reviewed revision; replies inherit it with thread state.
func (sh *ShamHub) storeReviewComment(
	comment shamComment,
	threadID ReviewThreadID,
	rootCommitHash git.Hash,
) shamComment {
	if comment.ID == 0 {
		comment.ID = sh.nextCommentID()
	}
	comment.Resolvable = true
	comment.ThreadID = threadID
	if threadID == "" {
		comment.ThreadID = ReviewThreadID(fmt.Sprintf("thread-%d", comment.ID))
		comment.CommitHash = rootCommitHash
	} else {
		root := sh.reviewThreadRoot(threadID)
		comment.Resolved = root.Resolved
		comment.CommitHash = root.CommitHash
	}
	sh.comments = append(sh.comments, comment)
	return comment
}

// validateSubmitReviewRequest rejects malformed path and JSON values without
// consulting ShamHub storage.
func validateSubmitReviewRequest(req *submitReviewRequest) error {
	if req.Body == "" &&
		len(req.Comments) == 0 &&
		req.Disposition == int(forge.ReviewDispositionNone) {
		return errors.New("submission must include a body, comment, or disposition")
	}
	if err := validateReviewDisposition(forge.ReviewDisposition(req.Disposition)); err != nil {
		return err
	}
	for i, comment := range req.Comments {
		if comment.Body == "" {
			return fmt.Errorf("comment %d body is required", i)
		}
		if comment.ThreadID != "" {
			continue
		}
		if comment.Path == "" {
			return fmt.Errorf("comment %d path is required", i)
		}
		reviewRange := reviewRangeFromRequest(comment)
		if reviewRange.IsZero() {
			continue
		}
		if err := validateReviewRange(reviewRange); err != nil {
			return fmt.Errorf("comment %d: %w", i, err)
		}
		if err := validateReviewSide(forge.ReviewThreadSide(comment.Side)); err != nil {
			return fmt.Errorf("comment %d: %w", i, err)
		}
	}
	return nil
}

// validateSubmitReviewThreadIdentities checks storage-owned thread identities
// while the caller holds sh.mu.
func (sh *ShamHub) validateSubmitReviewThreadIdentities(
	req *submitReviewRequest,
) error {
	for _, comment := range req.Comments {
		if comment.ThreadID == "" {
			continue
		}
		root := sh.reviewThreadRoot(ReviewThreadID(comment.ThreadID))
		if root == nil {
			return fmt.Errorf("thread %q not found", comment.ThreadID)
		}
		if root.Change != req.Change {
			return fmt.Errorf(
				"thread %q does not belong to change %d",
				comment.ThreadID,
				req.Change,
			)
		}
	}
	return nil
}

// reviewRangeFromRequest reconstructs the inclusive domain range from the flat
// request fields. An absent line and range identify the whole file.
func reviewRangeFromRequest(req submitReviewCommentRequest) forge.ReviewThreadRange {
	return reviewRangeFromCoordinates(req.Line, req.RangeStart, req.RangeEnd)
}

// reviewRangeFromCoordinates converts ShamHub's flat location fields to the
// forge range. Explicit endpoints take precedence over line; without endpoints,
// a positive line identifies one line, while all-zero coordinates identify the
// whole file.
func reviewRangeFromCoordinates(line, rangeStart, rangeEnd int) forge.ReviewThreadRange {
	if rangeStart != 0 || rangeEnd != 0 {
		return forge.ReviewThreadRange{
			StartLine: rangeStart,
			EndLine:   rangeEnd,
		}
	}
	if line == 0 {
		return forge.ReviewThreadRange{}
	}
	return forge.ReviewThreadLine(line)
}

// reviewThreadRoot returns the first stored comment for a thread. Roots are
// appended before replies, so the first match owns the thread's coordinates.
func (sh *ShamHub) reviewThreadRoot(id ReviewThreadID) *shamComment {
	for i := range sh.comments {
		if sh.comments[i].ThreadID == id {
			return &sh.comments[i]
		}
	}
	return nil
}

// changeBelongsToRepository scopes review endpoints to the change's base
// repository, which supplies the owner and repository path parameters.
func (sh *ShamHub) changeBelongsToRepository(
	changeNumber int,
	owner string,
	repo string,
) bool {
	for _, change := range sh.changes {
		if change.Number == changeNumber &&
			change.Base.Owner == owner &&
			change.Base.Repo == repo {
			return true
		}
	}
	return false
}

// ListReviewThreads groups ShamHub's flat comment rows into review threads.
// First occurrence preserves thread order; row order preserves comment order.
func (r *forgeRepository) ListReviewThreads(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewThread, error] {
	return func(yield func(*forge.ReviewThread, error) bool) {
		u := r.apiURL.JoinPath(r.owner, r.repo, "review-threads")
		query := u.Query()
		query.Set("change", strconv.Itoa(int(id.(ChangeID))))
		u.RawQuery = query.Encode()

		var response listReviewThreadsResponse
		if err := r.client.Get(ctx, u.String(), &response); err != nil {
			yield(nil, fmt.Errorf("list review threads: %w", err))
			return
		}

		threadsByID := make(map[string]*forge.ReviewThread)
		var threads []*forge.ReviewThread
		for _, item := range response.Items {
			thread, ok := threadsByID[item.ThreadID]
			if !ok {
				resolved := item.Resolved
				outdated := item.Outdated
				thread = &forge.ReviewThread{
					ID:         ReviewThreadID(item.ThreadID),
					Path:       item.Path,
					Range:      reviewRangeFromItem(item),
					Side:       forge.ReviewThreadSide(item.Side),
					CommitHash: git.Hash(item.CommitHash),
					Resolved:   &resolved,
					Outdated:   &outdated,
				}
				threadsByID[item.ThreadID] = thread
				threads = append(threads, thread)
			}
			thread.Comments = append(thread.Comments, forge.ReviewComment{
				ID:        ReviewCommentID(item.ID),
				Body:      item.Body,
				Author:    item.Author,
				CreatedAt: item.CreatedAt,
			})
		}

		for _, thread := range threads {
			if !yield(thread, nil) {
				return
			}
		}
	}
}

type listReviewThreadsRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Change int    `form:"change,required" json:"-"`
}

type listReviewThreadsResponse struct {
	Items []reviewCommentItem `json:"items,omitempty"`
}

// reviewCommentItem is one flat protocol row. The root carries coordinates and
// the reviewed change head; replies share its thread ID and CommitHash while
// leaving their coordinate fields empty.
type reviewCommentItem struct {
	ID         int       `json:"id"`
	ThreadID   string    `json:"threadID"`
	Path       string    `json:"path"`
	Line       int       `json:"line,omitzero"`
	RangeStart int       `json:"rangeStart,omitzero"`
	RangeEnd   int       `json:"rangeEnd,omitzero"`
	Side       int       `json:"side,omitzero"`
	CommitHash string    `json:"commitHash,omitzero"`
	Body       string    `json:"body"`
	Author     string    `json:"author"`
	Resolved   bool      `json:"resolved"`
	Outdated   bool      `json:"outdated"`
	CreatedAt  time.Time `json:"createdAt"`
}

// reviewRangeFromItem reconstructs the inclusive domain range from a root row.
// An absent line and range identify the whole file.
func reviewRangeFromItem(item reviewCommentItem) forge.ReviewThreadRange {
	return reviewRangeFromCoordinates(item.Line, item.RangeStart, item.RangeEnd)
}

// handleListReviewThreads emits flat rows in storage order. Because roots are
// stored first, the forge adapter can recover thread metadata from the first row.
func (sh *ShamHub) handleListReviewThreads(
	ctx context.Context,
	req *listReviewThreadsRequest,
) (*listReviewThreadsResponse, error) {
	head, err := sh.resolveReviewHead(ctx, req.Owner, req.Repo, req.Change)
	if err != nil {
		return nil, err
	}

	sh.mu.RLock()
	var comments []shamComment
	for _, comment := range sh.comments {
		if comment.Change != req.Change || comment.ThreadID == "" {
			continue
		}
		comments = append(comments, comment)
	}
	sh.mu.RUnlock()

	var items []reviewCommentItem
	for _, comment := range comments {
		outdated := comment.Outdated
		if !outdated {
			outdated, err = sh.reviewCommentOutdated(ctx, comment, head)
			if err != nil {
				return nil, fmt.Errorf(
					"compute review comment %d staleness: %w",
					comment.ID,
					err,
				)
			}
		}
		items = append(items, reviewCommentItem{
			ID:         comment.ID,
			ThreadID:   comment.ThreadID.String(),
			Path:       comment.Path,
			Line:       comment.Line,
			RangeStart: comment.RangeStart,
			RangeEnd:   comment.RangeEnd,
			Side:       int(comment.Side),
			CommitHash: comment.CommitHash.String(),
			Body:       comment.Body,
			Author:     comment.Author,
			Resolved:   comment.Resolved,
			Outdated:   outdated,
			CreatedAt:  comment.CreatedAt,
		})
	}
	return &listReviewThreadsResponse{Items: items}, nil
}

// reviewCommentOutdated reports whether a later change deleted any line in a
// right-side root's inclusive range. A left-side or file-level root does not
// identify old-side lines in the reviewed revision, so ShamHub does not infer
// staleness for those anchors.
func (sh *ShamHub) reviewCommentOutdated(
	ctx context.Context,
	comment shamComment,
	head reviewHeadSnapshot,
) (bool, error) {
	reviewRange := reviewRangeFromCoordinates(
		comment.Line,
		comment.RangeStart,
		comment.RangeEnd,
	)
	if comment.CommitHash == "" ||
		comment.CommitHash == head.Hash ||
		comment.Path == "" ||
		comment.Side != forge.ReviewThreadSideRight ||
		reviewRange.IsZero() {
		return false, nil
	}
	if head.Owner == "" || head.Repo == "" {
		return false, errors.New("change head repository is missing")
	}

	out, err := sh.gitCmd(
		ctx,
		head.Owner,
		head.Repo,
		"diff",
		"--unified=0",
		comment.CommitHash.String()+".."+head.Hash.String(),
		"--",
		comment.Path,
	).Output()
	if err != nil {
		return false, fmt.Errorf("diff reviewed revision: %w", err)
	}

	patch, err := reviewdiff.Parse(out)
	if err != nil {
		return false, fmt.Errorf("parse reviewed revision diff: %w", err)
	}
	return patch.DeletesLineRange(
		comment.Path,
		reviewRange.StartLine,
		reviewRange.EndLine,
	), nil
}

// ListReviewerStates yields each reviewer's latest effective state in the
// stable reviewer order supplied by ShamHub.
func (r *forgeRepository) ListReviewerStates(
	ctx context.Context,
	id forge.ChangeID,
) iter.Seq2[*forge.ReviewerState, error] {
	return func(yield func(*forge.ReviewerState, error) bool) {
		u := r.apiURL.JoinPath(r.owner, r.repo, "reviews")
		query := u.Query()
		query.Set("change", strconv.Itoa(int(id.(ChangeID))))
		u.RawQuery = query.Encode()

		var response listReviewerStatesResponse
		if err := r.client.Get(ctx, u.String(), &response); err != nil {
			yield(nil, fmt.Errorf("list reviewer states: %w", err))
			return
		}
		for _, state := range response.States {
			if !yield(&forge.ReviewerState{
				Reviewer:    state.Reviewer,
				Disposition: forge.ReviewDisposition(state.Disposition),
				CommitHash:  git.Hash(state.CommitHash),
				SubmittedAt: state.SubmittedAt,
			}, nil) {
				return
			}
		}
	}
}

// shamFeedbackSubmission records one SubmitReview request.
// A comment submission has no disposition and creates no reviewer state.
type shamFeedbackSubmission struct {
	Change      int
	Submitter   string
	Disposition forge.ReviewDisposition
	Body        string
	CommentIDs  []int
	SubmittedAt time.Time
}

type listReviewerStatesRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Change int    `form:"change,required" json:"-"`
}

type listReviewerStatesResponse struct {
	States []reviewerStateItem `json:"states,omitempty"`
}

type reviewerStateItem struct {
	Reviewer    string    `json:"reviewer"`
	Disposition int       `json:"disposition,omitzero"`
	CommitHash  string    `json:"commitHash,omitzero"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// handleListReviewerStates keeps the first effective-state position for each
// reviewer while replacing it with every later effective disposition.
func (sh *ShamHub) handleListReviewerStates(
	_ context.Context,
	req *listReviewerStatesRequest,
) (*listReviewerStatesResponse, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	if !sh.changeBelongsToRepository(req.Change, req.Owner, req.Repo) {
		return nil, notFoundErrorf(
			"change %d not found in %s/%s",
			req.Change,
			req.Owner,
			req.Repo,
		)
	}

	stateByReviewer := make(map[string]int)
	var states []reviewerStateItem
	for _, submission := range sh.feedbackSubmissions {
		if submission.Change != req.Change ||
			submission.Disposition == forge.ReviewDispositionNone {
			continue
		}
		state := reviewerStateItem{
			Reviewer:    submission.Submitter,
			Disposition: int(submission.Disposition),
			SubmittedAt: submission.SubmittedAt,
		}
		if index, ok := stateByReviewer[submission.Submitter]; ok {
			states[index] = state
			continue
		}
		stateByReviewer[submission.Submitter] = len(states)
		states = append(states, state)
	}
	return &listReviewerStatesResponse{States: states}, nil
}

// UpdateReviewComment replaces a ShamHub review comment body.
func (r *forgeRepository) UpdateReviewComment(
	ctx context.Context,
	id forge.ReviewCommentID,
	body string,
) error {
	return r.updateComment(ctx, int(mustReviewCommentID(id)), body)
}

// ResolveReviewThread marks a ShamHub review thread as resolved.
func (r *forgeRepository) ResolveReviewThread(
	ctx context.Context,
	id forge.ReviewThreadID,
) error {
	return r.setReviewThreadResolved(ctx, id, true)
}

// UnresolveReviewThread marks a ShamHub review thread as unresolved.
func (r *forgeRepository) UnresolveReviewThread(
	ctx context.Context,
	id forge.ReviewThreadID,
) error {
	return r.setReviewThreadResolved(ctx, id, false)
}

// setReviewThreadResolved converts the typed thread ID and selects the protocol
// endpoint whose handler owns the storage mutation.
func (r *forgeRepository) setReviewThreadResolved(
	ctx context.Context,
	id forge.ReviewThreadID,
	resolved bool,
) error {
	threadID := mustReviewThreadID(id)

	action := "resolve"
	if !resolved {
		action = "unresolve"
	}
	u := r.apiURL.JoinPath(
		r.owner,
		r.repo,
		"threads",
		threadID.String(),
		action,
	)
	var response reviewThreadStateResponse
	if err := r.client.Post(ctx, u.String(), struct{}{}, &response); err != nil {
		return fmt.Errorf("%s review thread: %w", action, err)
	}
	return nil
}

type reviewThreadStateRequest struct {
	Owner    string `path:"owner" json:"-"`
	Repo     string `path:"repo" json:"-"`
	ThreadID string `path:"threadID" json:"-"`
}

type reviewThreadStateResponse struct{}

// The endpoint-specific handlers select a target state; handleReviewThreadState
// owns lookup, repository scoping, and storage mutation for both endpoints.
func (sh *ShamHub) handleResolveReviewThread(
	ctx context.Context,
	req *reviewThreadStateRequest,
) (*reviewThreadStateResponse, error) {
	return sh.handleReviewThreadState(ctx, req, true)
}

func (sh *ShamHub) handleUnresolveReviewThread(
	ctx context.Context,
	req *reviewThreadStateRequest,
) (*reviewThreadStateResponse, error) {
	return sh.handleReviewThreadState(ctx, req, false)
}

// handleReviewThreadState updates every stored comment in the thread so each
// flat row reports the same resolution state on subsequent list requests.
func (sh *ShamHub) handleReviewThreadState(
	_ context.Context,
	req *reviewThreadStateRequest,
	resolved bool,
) (*reviewThreadStateResponse, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	root := sh.reviewThreadRoot(ReviewThreadID(req.ThreadID))
	if root == nil ||
		!sh.changeBelongsToRepository(root.Change, req.Owner, req.Repo) {
		return nil, notFoundErrorf("thread %q not found", req.ThreadID)
	}
	for i := range sh.comments {
		if sh.comments[i].ThreadID == ReviewThreadID(req.ThreadID) {
			sh.comments[i].Resolved = resolved
		}
	}
	return &reviewThreadStateResponse{}, nil
}
