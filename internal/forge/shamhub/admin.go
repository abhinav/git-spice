package shamhub

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"go.abhg.dev/gs/internal/forge"
)

var (
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/users",
		(*ShamHub).handleAdminRegisterUser,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/repos",
		(*ShamHub).handleAdminNewRepository,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/repos/fork",
		(*ShamHub).handleAdminForkRepository,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/config",
		(*ShamHub).handleAdminConfig,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/merge",
		(*ShamHub).handleAdminMergeChange,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/reject",
		(*ShamHub).handleAdminRejectChange,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/checks",
		(*ShamHub).handleAdminSetStatus,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/changes/{owner}/{repo}/{number}/mergeability",
		(*ShamHub).handleAdminSetMergeability,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/comments",
		(*ShamHub).handleAdminPostComment,
	)
	_ = shamhubRESTHandler(
		"PATCH /_shamhub/admin/comments/{id}",
		(*ShamHub).handleAdminEditComment,
	)
	_ = shamhubRESTHandler(
		"DELETE /_shamhub/admin/comments/{id}",
		(*ShamHub).handleAdminDeleteComment,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/reviews",
		(*ShamHub).handleAdminSubmitFeedback,
	)
	_ = shamhubRESTHandler(
		"POST /_shamhub/admin/review-comments",
		(*ShamHub).handleAdminPostReviewComment,
	)
	_ = shamhubRESTHandler(
		"GET /_shamhub/admin/dump/changes",
		(*ShamHub).handleAdminDumpChanges,
	)
	_ = shamhubRESTHandler(
		"GET /_shamhub/admin/dump/changes/{number}",
		(*ShamHub).handleAdminDumpChange,
	)
	_ = shamhubHTTPHandler(
		"GET /_shamhub/admin/dump/comments",
		(*ShamHub).handleAdminDumpComments,
	)
	_ = shamhubHTTPHandler(
		"GET /_shamhub/admin/dump/reviews",
		(*ShamHub).handleAdminDumpReviews,
	)
)

func shamhubHTTPHandler(
	pattern string,
	handler func(*ShamHub, http.ResponseWriter, *http.Request),
) struct{} {
	_handlers = append(_handlers, shamhubEndpoint{
		Pattern: pattern,
		Handler: func(sh *ShamHub, w http.ResponseWriter, r *http.Request) {
			handler(sh, w, r)
		},
	})
	return struct{}{}
}

type adminRegisterUserBody struct {
	Username string `json:"username"`
}

type adminRegisterUserRequest struct {
	Username string `json:"username"`
}

type adminRegisterUserResponse struct{}

// User administration creates identities that can later log in.
func (sh *ShamHub) handleAdminRegisterUser(
	_ context.Context,
	req adminRegisterUserRequest,
) (*adminRegisterUserResponse, error) {
	if err := sh.RegisterUser(req.Username); err != nil {
		return nil, err
	}
	return &adminRegisterUserResponse{}, nil
}

type adminNewRepositoryBody struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type adminNewRepositoryRequest struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type adminRepositoryResponse struct {
	URL string `json:"url"`
}

// Repository administration creates a bare Git repository and returns its URL.
func (sh *ShamHub) handleAdminNewRepository(
	_ context.Context,
	req adminNewRepositoryRequest,
) (*adminRepositoryResponse, error) {
	url, err := sh.NewRepository(req.Owner, req.Repo)
	if err != nil {
		return nil, err
	}
	return &adminRepositoryResponse{URL: url}, nil
}

type adminForkRepositoryBody struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	ForkOwner string `json:"forkOwner"`
}

type adminForkRepositoryRequest struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	ForkOwner string `json:"forkOwner"`
}

// Repository forking copies an existing bare repository to a new owner.
func (sh *ShamHub) handleAdminForkRepository(
	_ context.Context,
	req adminForkRepositoryRequest,
) (*adminRepositoryResponse, error) {
	url, err := sh.ForkRepository(req.Owner, req.Repo, req.ForkOwner)
	if err != nil {
		return nil, err
	}
	return &adminRepositoryResponse{URL: url}, nil
}

type adminConfigBody struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type adminConfigRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type adminConfigResponse struct{}

// Configuration updates mutate ShamHub server knobs used by tests.
func (sh *ShamHub) handleAdminConfig(
	_ context.Context,
	req adminConfigRequest,
) (*adminConfigResponse, error) {
	switch req.Key {
	case "changeTemplateErrorDelay":
		delay, err := time.ParseDuration(req.Value)
		if err != nil {
			return nil, badRequestErrorf("parse duration: %s", err)
		}

		sh.mu.Lock()
		sh.changeTemplateErrorDelay = delay
		sh.mu.Unlock()

	case "mergeMethod":
		mergeMethod, err := parseMergeMethod(req.Value)
		if err != nil {
			return nil, badRequestErrorf("%s", err)
		}

		sh.mu.Lock()
		sh.defaultMergeMethod = mergeMethod
		sh.mu.Unlock()

	default:
		return nil, badRequestErrorf("unknown config key %q", req.Key)
	}

	return &adminConfigResponse{}, nil
}

type adminMergeChangeBody struct {
	Time           time.Time `json:"time"`
	CommitterName  string    `json:"committerName,omitzero"`
	CommitterEmail string    `json:"committerEmail,omitzero"`
	DeleteBranch   bool      `json:"deleteBranch,omitzero"`
	Squash         bool      `json:"squash,omitzero"`
}

type adminMergeChangeRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	Time           time.Time `json:"time"`
	CommitterName  string    `json:"committerName,omitzero"`
	CommitterEmail string    `json:"committerEmail,omitzero"`
	DeleteBranch   bool      `json:"deleteBranch,omitzero"`
	Squash         bool      `json:"squash,omitzero"`
}

type adminMergeChangeResponse struct{}

// Change merging drives the same merge operation that forge clients observe.
func (sh *ShamHub) handleAdminMergeChange(
	_ context.Context,
	req adminMergeChangeRequest,
) (*adminMergeChangeResponse, error) {
	if err := sh.MergeChange(MergeChangeRequest{
		Owner:          req.Owner,
		Repo:           req.Repo,
		Number:         req.Number,
		Time:           req.Time,
		CommitterName:  req.CommitterName,
		CommitterEmail: req.CommitterEmail,
		DeleteBranch:   req.DeleteBranch,
		Squash:         req.Squash,
	}); err != nil {
		return nil, err
	}
	return &adminMergeChangeResponse{}, nil
}

type adminRejectChangeRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`
}

type adminRejectChangeBody struct{}

type adminRejectChangeResponse struct{}

// Change rejection closes a change without merging it.
func (sh *ShamHub) handleAdminRejectChange(
	_ context.Context,
	req adminRejectChangeRequest,
) (*adminRejectChangeResponse, error) {
	if err := sh.RejectChange(RejectChangeRequest(req)); err != nil {
		return nil, err
	}
	return &adminRejectChangeResponse{}, nil
}

type adminSetStatusBody struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type adminSetStatusRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	Name  string `json:"name"`
	State string `json:"state"`
}

type adminSetStatusResponse struct{}

// Check administration sets the latest named status for a change.
func (sh *ShamHub) handleAdminSetStatus(
	_ context.Context,
	req adminSetStatusRequest,
) (*adminSetStatusResponse, error) {
	state, err := parseChecksState(req.State)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeCheck(
		req.Owner,
		req.Repo,
		req.Number,
		forge.ChangeCheck{Name: req.Name, State: state},
	); err != nil {
		return nil, err
	}
	return &adminSetStatusResponse{}, nil
}

type adminSetMergeabilityBody struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitzero"`
}

type adminSetMergeabilityRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	State  string `json:"state"`
	Reason string `json:"reason,omitzero"`
}

type adminSetMergeabilityResponse struct{}

// Mergeability administration sets the merge gate state for a change.
func (sh *ShamHub) handleAdminSetMergeability(
	_ context.Context,
	req adminSetMergeabilityRequest,
) (*adminSetMergeabilityResponse, error) {
	mergeability, err := parseMergeability(req.State, req.Reason)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeMergeability(SetChangeMergeabilityRequest{
		Owner:        req.Owner,
		Repo:         req.Repo,
		Number:       req.Number,
		Mergeability: mergeability,
	}); err != nil {
		return nil, err
	}
	return &adminSetMergeabilityResponse{}, nil
}

type adminPostCommentBody struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	Change     int    `json:"change"`
	ID         int    `json:"id,omitzero"`
	Body       string `json:"body"`
	Resolvable bool   `json:"resolvable,omitzero"`
	Resolved   bool   `json:"resolved,omitzero"`
}

type adminPostCommentRequest struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	Change     int    `json:"change"`
	ID         int    `json:"id,omitzero"`
	Body       string `json:"body"`
	Resolvable bool   `json:"resolvable,omitzero"`
	Resolved   bool   `json:"resolved,omitzero"`
}

type adminPostCommentResponse struct {
	ID int `json:"id"`
}

// Comment creation supports explicit IDs for deterministic test fixtures.
func (sh *ShamHub) handleAdminPostComment(
	_ context.Context,
	req adminPostCommentRequest,
) (*adminPostCommentResponse, error) {
	id, err := sh.PostComment(PostCommentRequest(req))
	if err != nil {
		return nil, err
	}
	return &adminPostCommentResponse{ID: id}, nil
}

type adminEditCommentBody struct {
	Resolved *bool `json:"resolved,omitzero"`
}

type adminEditCommentRequest struct {
	ID int `path:"id" json:"-"`

	Resolved *bool `json:"resolved,omitzero"`
}

type adminEditCommentResponse struct{}

// Comment editing currently updates resolution state only.
func (sh *ShamHub) handleAdminEditComment(
	_ context.Context,
	req adminEditCommentRequest,
) (*adminEditCommentResponse, error) {
	if err := sh.EditComment(EditCommentRequest(req)); err != nil {
		return nil, err
	}
	return &adminEditCommentResponse{}, nil
}

type adminDeleteCommentRequest struct {
	ID int `path:"id" json:"-"`
}

type adminDeleteCommentResponse struct{}

// Comment deletion removes a seeded or forge-created comment by ID.
func (sh *ShamHub) handleAdminDeleteComment(
	_ context.Context,
	req adminDeleteCommentRequest,
) (*adminDeleteCommentResponse, error) {
	if err := sh.DeleteComment(req.ID); err != nil {
		return nil, err
	}
	return &adminDeleteCommentResponse{}, nil
}

type adminSubmitFeedbackBody struct {
	Owner       string    `json:"owner"`
	Repo        string    `json:"repo"`
	Change      int       `json:"change"`
	Submitter   string    `json:"submitter"`
	Disposition int       `json:"disposition"`
	Body        string    `json:"body,omitzero"`
	Time        time.Time `json:"time"`
}

type adminSubmitFeedbackResponse struct{}

// handleAdminSubmitFeedback records a feedback submission without requiring
// forge authentication.
func (sh *ShamHub) handleAdminSubmitFeedback(
	_ context.Context,
	req adminSubmitFeedbackBody,
) (*adminSubmitFeedbackResponse, error) {
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
	submit := &submitReviewRequest{
		Owner:       req.Owner,
		Repo:        req.Repo,
		Change:      req.Change,
		Body:        req.Body,
		Disposition: req.Disposition,
	}
	if err := validateSubmitReviewRequest(submit); err != nil {
		return nil, badRequestErrorf("%v", err)
	}
	if req.Time.IsZero() {
		req.Time = time.Now()
	}
	if _, err := sh.submitReview(req.Submitter, submit, "", req.Time); err != nil {
		return nil, err
	}
	return &adminSubmitFeedbackResponse{}, nil
}

type adminPostReviewCommentBody struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	Change     int       `json:"change"`
	ID         int       `json:"id,omitzero"`
	Author     string    `json:"author"`
	Path       string    `json:"path,omitzero"`
	RangeStart int       `json:"rangeStart,omitzero"`
	RangeEnd   int       `json:"rangeEnd,omitzero"`
	Side       int       `json:"side,omitzero"`
	ThreadID   string    `json:"threadID,omitzero"`
	Body       string    `json:"body"`
	Resolved   bool      `json:"resolved,omitzero"`
	Outdated   bool      `json:"outdated,omitzero"`
	Time       time.Time `json:"time"`
}

type adminPostReviewCommentResponse struct {
	ID       int    `json:"id"`
	ThreadID string `json:"threadID"`
}

// Review-comment administration seeds a review thread root or reply.
func (sh *ShamHub) handleAdminPostReviewComment(
	ctx context.Context,
	req adminPostReviewCommentBody,
) (*adminPostReviewCommentResponse, error) {
	reviewRange := forge.ReviewThreadRange{
		StartLine: req.RangeStart,
		EndLine:   req.RangeEnd,
	}
	if req.Body == "" {
		return nil, badRequestErrorf("review comment body is required")
	}
	if req.ThreadID == "" {
		if req.Path == "" {
			return nil, badRequestErrorf("review comment path is required")
		}
		if !reviewRange.IsZero() {
			if err := validateReviewRange(reviewRange); err != nil {
				return nil, badRequestErrorf("%v", err)
			}
			if err := validateReviewSide(forge.ReviewThreadSide(req.Side)); err != nil {
				return nil, badRequestErrorf("%v", err)
			}
		}
	}

	rootCommitHash, err := sh.reviewRootCommitHash(
		ctx,
		req.Owner,
		req.Repo,
		req.Change,
		req.ThreadID == "",
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
	threadID := ReviewThreadID(req.ThreadID)
	if threadID != "" {
		root := sh.reviewThreadRoot(threadID)
		if root == nil || root.Change != req.Change {
			return nil, notFoundErrorf("thread %q not found", req.ThreadID)
		}
	}

	if req.ID != 0 && sh.commentByID(req.ID) != nil {
		return nil, badRequestErrorf("comment %d already exists", req.ID)
	}
	if req.Time.IsZero() {
		req.Time = time.Now()
	}
	comment := shamComment{
		ID:        req.ID,
		Change:    req.Change,
		Body:      req.Body,
		Resolved:  req.Resolved,
		Outdated:  req.Outdated,
		Author:    req.Author,
		CreatedAt: req.Time,
	}
	if req.ThreadID == "" {
		comment.Path = req.Path
		if !reviewRange.IsZero() {
			comment.Line = req.RangeStart
			comment.RangeStart = req.RangeStart
			comment.RangeEnd = req.RangeEnd
			comment.Side = forge.ReviewThreadSide(req.Side)
		}
	}
	comment = sh.storeReviewComment(comment, threadID, rootCommitHash)
	return &adminPostReviewCommentResponse{
		ID:       comment.ID,
		ThreadID: comment.ThreadID.String(),
	}, nil
}

type adminDumpChangesRequest struct{}

type adminDumpChangesResponse struct {
	Changes []*Change `json:"changes"`
}

// Change dumps return all change records for golden-file comparisons.
func (sh *ShamHub) handleAdminDumpChanges(
	_ context.Context,
	_ adminDumpChangesRequest,
) (*adminDumpChangesResponse, error) {
	changes, err := sh.ListChanges()
	if err != nil {
		return nil, err
	}
	return &adminDumpChangesResponse{Changes: changes}, nil
}

type adminDumpChangeRequest struct {
	Number int `path:"number" json:"-"`
}

type adminDumpChangeResponse struct {
	Change *Change `json:"change"`
}

// Single-change dumps return one change by number for CLI assertions.
func (sh *ShamHub) handleAdminDumpChange(
	_ context.Context,
	req adminDumpChangeRequest,
) (*adminDumpChangeResponse, error) {
	changes, err := sh.ListChanges()
	if err != nil {
		return nil, err
	}
	idx := slices.IndexFunc(changes, func(change *Change) bool {
		return change.Number == req.Number
	})
	if idx < 0 {
		return nil, notFoundErrorf("change %d not found", req.Number)
	}
	return &adminDumpChangeResponse{Change: changes[idx]}, nil
}

type adminDumpCommentsResponse struct {
	Comments []*ChangeComment `json:"comments"`
}

// Comment dumps keep repeated change query parameters for script ergonomics.
func (sh *ShamHub) handleAdminDumpComments(
	w http.ResponseWriter,
	r *http.Request,
) {
	comments, err := sh.ListChangeComments()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	changeIDs := make(map[int]struct{})
	for _, value := range r.URL.Query()["change"] {
		change, err := strconv.Atoi(value)
		if err != nil {
			http.Error(w, "invalid change query", http.StatusBadRequest)
			return
		}
		changeIDs[change] = struct{}{}
	}

	if len(changeIDs) > 0 {
		comments = slices.DeleteFunc(comments, func(comment *ChangeComment) bool {
			_, ok := changeIDs[comment.Change]
			return !ok
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, adminDumpCommentsResponse{
		Comments: comments,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type adminDumpFeedbackResponse struct {
	Changes []adminDumpFeedbackChange `json:"changes" yaml:"changes"`
}

type adminDumpFeedbackChange struct {
	Change      int                           `json:"change" yaml:"change"`
	Submissions []adminDumpFeedbackSubmission `json:"submissions" yaml:"submissions"`
	Threads     []adminDumpReviewThread       `json:"threads" yaml:"threads"`
}

type adminDumpFeedbackSubmission struct {
	Submitter   string `json:"submitter" yaml:"submitter"`
	Disposition string `json:"disposition" yaml:"disposition"`
	Body        string `json:"body,omitzero" yaml:"body,omitempty"`
	CommentIDs  []int  `json:"commentIDs,omitempty" yaml:"commentIDs,omitempty"`
}

type adminDumpReviewThread struct {
	ID       string                   `json:"id" yaml:"id"`
	Path     string                   `json:"path" yaml:"path"`
	Range    *adminDumpReviewRange    `json:"range,omitempty" yaml:"range,omitempty"`
	Side     string                   `json:"side,omitempty" yaml:"side,omitempty"`
	Resolved bool                     `json:"resolved" yaml:"resolved"`
	Outdated bool                     `json:"outdated" yaml:"outdated"`
	Comments []adminDumpReviewComment `json:"comments" yaml:"comments"`
}

type adminDumpReviewRange struct {
	Start int `json:"start" yaml:"start"`
	End   int `json:"end" yaml:"end"`
}

type adminDumpReviewComment struct {
	ID     int    `json:"id" yaml:"id"`
	Author string `json:"author" yaml:"author"`
	Body   string `json:"body" yaml:"body"`
}

// Feedback dumps preserve the storage order of changes, submissions,
// and comments while omitting nondeterministic metadata.
func (sh *ShamHub) handleAdminDumpReviews(
	w http.ResponseWriter,
	r *http.Request,
) {
	changeIDs, err := adminDumpChangeIDs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.RLock()
	defer sh.mu.RUnlock()

	changes := make(map[int]*adminDumpFeedbackChange)
	appendChange := func(id int) *adminDumpFeedbackChange {
		if change, ok := changes[id]; ok {
			return change
		}
		change := &adminDumpFeedbackChange{Change: id}
		changes[id] = change
		return change
	}
	for _, submission := range sh.feedbackSubmissions {
		if len(changeIDs) > 0 {
			if _, ok := changeIDs[submission.Change]; !ok {
				continue
			}
		}
		change := appendChange(submission.Change)
		// A feedback submission owns this grouping, including IDs of comments
		// it created. The comments alone cannot reconstruct that submission.
		change.Submissions = append(change.Submissions, adminDumpFeedbackSubmission{
			Submitter:   submission.Submitter,
			Disposition: reviewDispositionName(submission.Disposition),
			Body:        submission.Body,
			CommentIDs:  submission.CommentIDs,
		})
	}
	type threadLocation struct {
		change *adminDumpFeedbackChange
		index  int
	}
	// Store locations by index rather than pointers because appending a later
	// thread can reallocate a change's Threads slice.
	threads := make(map[ReviewThreadID]threadLocation)
	for _, comment := range sh.comments {
		if comment.ThreadID == "" {
			continue
		}
		if len(changeIDs) > 0 {
			if _, ok := changeIDs[comment.Change]; !ok {
				continue
			}
		}
		change := appendChange(comment.Change)
		location, ok := threads[comment.ThreadID]
		if !ok {
			thread := adminDumpReviewThread{
				ID:       comment.ThreadID.String(),
				Path:     comment.Path,
				Resolved: comment.Resolved,
				Outdated: comment.Outdated,
			}
			reviewRange := reviewRangeFromCoordinates(
				comment.Line,
				comment.RangeStart,
				comment.RangeEnd,
			)
			if !reviewRange.IsZero() {
				thread.Range = &adminDumpReviewRange{
					Start: reviewRange.StartLine,
					End:   reviewRange.EndLine,
				}
				thread.Side = comment.Side.String()
			}
			change.Threads = append(change.Threads, thread)
			location = threadLocation{change: change, index: len(change.Threads) - 1}
			threads[comment.ThreadID] = location
		}
		thread := &location.change.Threads[location.index]
		thread.Comments = append(thread.Comments, adminDumpReviewComment{
			ID:     comment.ID,
			Author: comment.Author,
			Body:   comment.Body,
		})
	}

	var result adminDumpFeedbackResponse
	for _, change := range sh.changes {
		if dumped, ok := changes[change.Number]; ok {
			result.Changes = append(result.Changes, *dumped)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func adminDumpChangeIDs(r *http.Request) (map[int]struct{}, error) {
	ids := make(map[int]struct{})
	for _, value := range r.URL.Query()["change"] {
		id, err := strconv.Atoi(value)
		if err != nil {
			return nil, errors.New("invalid change query")
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}

func reviewDispositionName(disposition forge.ReviewDisposition) string {
	switch disposition {
	case forge.ReviewDispositionNone:
		return "comment"
	case forge.ReviewDispositionApprove:
		return "approve"
	case forge.ReviewDispositionRequestChanges:
		return "request-changes"
	default:
		return strconv.Itoa(int(disposition))
	}
}
