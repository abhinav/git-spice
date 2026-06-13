package shamhub

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.abhg.dev/gs/internal/diffmap"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/xec"
)

var (
	_ forge.WithInlineComments   = (*forgeRepository)(nil)
	_ forge.WithThreadResolution = (*forgeRepository)(nil)
	_ forge.WithCommentEdit      = (*forgeRepository)(nil)
)

// Inline comment handlers

var (
	_ = shamhubRESTHandler(
		"GET /{owner}/{repo}/inline-comments",
		(*ShamHub).handleListInlineComments,
	)
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/inline-comments",
		(*ShamHub).handlePostInlineComment,
	)
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/reviews",
		(*ShamHub).handleSubmitReview,
	)
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/threads/{threadID}/resolve",
		(*ShamHub).handleResolveThread,
	)
	_ = shamhubRESTHandler(
		"POST /{owner}/{repo}/threads/{threadID}/unresolve",
		(*ShamHub).handleUnresolveThread,
	)
	_ = shamhubRESTHandler(
		"PATCH /{owner}/{repo}/comments/{id}/edit",
		(*ShamHub).handleEditComment,
	)
)

// List inline comments

type listInlineCommentsRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Change int    `form:"change,required" json:"-"`
}

type listInlineCommentsResponse struct {
	Items []inlineCommentItem `json:"items"`
}

type inlineCommentItem struct {
	ID        int       `json:"id"`
	ThreadID  string    `json:"threadID"`
	Scope     string    `json:"scope,omitempty"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
	RangeLo   int       `json:"rangeStart,omitempty"`
	RangeHi   int       `json:"rangeEnd,omitempty"`
	Side      string    `json:"side,omitempty"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	Resolved  bool      `json:"resolved"`
	Outdated  bool      `json:"outdated"`
	CreatedAt time.Time `json:"createdAt"`
}

func (sh *ShamHub) handleListInlineComments(
	ctx context.Context,
	req *listInlineCommentsRequest,
) (*listInlineCommentsResponse, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	// Locate the change once so the per-comment stale check has a
	// reference to compare each comment's anchor SHA against.
	var change *shamChange
	for i, c := range sh.changes {
		if c.Base.Owner == req.Owner &&
			c.Base.Repo == req.Repo &&
			c.Number == req.Change {
			change = &sh.changes[i]
			break
		}
	}

	var items []inlineCommentItem
	for _, c := range sh.comments {
		if c.Change != req.Change || c.Scope == "" {
			continue
		}
		outdated := c.Outdated
		if !outdated && change != nil {
			stale, err := sh.commentIsStale(ctx, &c, change)
			if err != nil {
				sh.log.Warn("compute stale",
					"comment", c.ID,
					"error", err)
			} else {
				outdated = stale
			}
		}
		item := inlineCommentItem{
			ID:        c.ID,
			ThreadID:  c.ThreadID,
			Scope:     c.Scope,
			Path:      c.Path,
			Line:      c.Line,
			RangeLo:   c.RangeStart,
			RangeHi:   c.RangeEnd,
			Side:      c.Side,
			Body:      c.Body,
			Author:    c.Author,
			Resolved:  c.Resolved,
			Outdated:  outdated,
			CreatedAt: c.CreatedAt,
		}
		items = append(items, item)
	}

	return &listInlineCommentsResponse{Items: items}, nil
}

// commentIsStale reports whether the comment's anchored line has
// been touched between the comment's recorded CommitSHA and the
// change's current head. Comments without a recorded CommitSHA
// (e.g. test-seeded entries) are never considered stale by this
// path; tests can still force stale via the Outdated override.
func (sh *ShamHub) commentIsStale(
	ctx context.Context, c *shamComment, change *shamChange,
) (bool, error) {
	if c.CommitSHA == "" {
		return false, nil
	}
	headSHA, err := sh.resolveBranchSHA(
		ctx, change.Head.Owner, change.Head.Repo, change.Head.Name,
	)
	if err != nil {
		return false, err
	}
	if headSHA == c.CommitSHA {
		return false, nil
	}

	out, err := xec.Command(ctx, sh.log, sh.gitExe,
		"diff", "--unified=0",
		c.CommitSHA+".."+headSHA, "--", c.Path).
		WithDir(sh.repoDir(change.Head.Owner, change.Head.Repo)).
		Output()
	if err != nil {
		// If the file is gone or the diff fails, treat as stale:
		// the comment's anchor can no longer be located cleanly.
		return true, nil
	}
	if len(out) == 0 {
		return false, nil
	}

	mapper, err := diffmap.New(out)
	if err != nil {
		return false, fmt.Errorf("parse diff: %w", err)
	}
	side := c.Side
	if side == "" {
		side = "RIGHT"
	}
	return mapper.LineModified(c.Path, c.Line, side), nil
}

// resolveBranchSHA returns the current SHA of the named branch in
// the given owner/repo's worktree.
func (sh *ShamHub) resolveBranchSHA(
	ctx context.Context, owner, repo, branch string,
) (string, error) {
	out, err := xec.Command(ctx, sh.log, sh.gitExe, "rev-parse", branch).
		WithDir(sh.repoDir(owner, repo)).
		Output()
	if err != nil {
		return "", fmt.Errorf(
			"resolve %s/%s:%s: %w", owner, repo, branch, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *forgeRepository) ListInlineComments(
	ctx context.Context,
	id forge.ChangeID,
) ([]*forge.InlineComment, error) {
	u := r.apiURL.JoinPath(r.owner, r.repo, "inline-comments")
	q := u.Query()
	q.Set("change", strconv.Itoa(int(id.(ChangeID))))
	u.RawQuery = q.Encode()

	var res listInlineCommentsResponse
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("list inline comments: %w", err)
	}

	var comments []*forge.InlineComment
	for _, item := range res.Items {
		c := &forge.InlineComment{
			ID:        ChangeCommentID(item.ID),
			ThreadID:  item.ThreadID,
			Scope:     parseCommentScope(item.Scope),
			Path:      item.Path,
			Line:      item.Line,
			Side:      item.Side,
			Body:      item.Body,
			Author:    item.Author,
			Resolved:  item.Resolved,
			Outdated:  item.Outdated,
			CreatedAt: item.CreatedAt,
		}
		if item.RangeLo != 0 || item.RangeHi != 0 {
			c.Range = &forge.CommentRange{
				Start: item.RangeLo,
				End:   item.RangeHi,
			}
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// parseCommentScope decodes the wire string used by shamhub
// into the typed [forge.CommentScope].
func parseCommentScope(s string) forge.CommentScope {
	switch s {
	case "pr":
		return forge.CommentScopePR
	case "file":
		return forge.CommentScopeFile
	case "line", "":
		return forge.CommentScopeLine
	}
	return forge.CommentScopeLine
}

// Post inline comment

type postInlineCommentRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Change     int    `json:"change"`
	Scope      string `json:"scope,omitempty"`
	Path       string `json:"path,omitempty"`
	Line       int    `json:"line,omitempty"`
	RangeStart int    `json:"rangeStart,omitempty"`
	RangeEnd   int    `json:"rangeEnd,omitempty"`
	Body       string `json:"body"`
	Side       string `json:"side,omitempty"`
	ThreadID   string `json:"threadID,omitempty"`
}

type postInlineCommentResponse struct {
	ID        int       `json:"id"`
	ThreadID  string    `json:"threadID"`
	CreatedAt time.Time `json:"createdAt"`
}

func (sh *ShamHub) handlePostInlineComment(
	ctx context.Context,
	req *postInlineCommentRequest,
) (*postInlineCommentResponse, error) {
	// Look up the change's head branch BEFORE taking the write lock
	// so the rev-parse subprocess doesn't fight other writers.
	sh.mu.RLock()
	var headOwner, headRepo, headBranch string
	for _, c := range sh.changes {
		if c.Base.Owner == req.Owner &&
			c.Base.Repo == req.Repo &&
			c.Number == req.Change {
			headOwner = c.Head.Owner
			headRepo = c.Head.Repo
			headBranch = c.Head.Name
			break
		}
	}
	sh.mu.RUnlock()

	var commitSHA string
	if headBranch != "" {
		sha, err := sh.resolveBranchSHA(ctx, headOwner, headRepo, headBranch)
		if err != nil {
			sh.log.Warn("capture comment commitSHA",
				"change", req.Change,
				"error", err)
		} else {
			commitSHA = sha
		}
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	threadID := req.ThreadID
	if threadID == "" {
		// New thread: generate a thread ID.
		threadID = fmt.Sprintf("thread-%d", len(sh.comments)+1)
	}

	scope := req.Scope
	if scope == "" {
		scope = "line"
	}
	now := time.Now()
	comment := shamComment{
		ID:         len(sh.comments) + 1,
		Change:     req.Change,
		Body:       req.Body,
		Path:       req.Path,
		Line:       req.Line,
		RangeStart: req.RangeStart,
		RangeEnd:   req.RangeEnd,
		Side:       req.Side,
		Scope:      scope,
		CommitSHA:  commitSHA,
		ThreadID:   threadID,
		Resolvable: true,
		Author:     "test-user",
		CreatedAt:  now,
	}
	sh.comments = append(sh.comments, comment)

	return &postInlineCommentResponse{
		ID:        comment.ID,
		ThreadID:  threadID,
		CreatedAt: now,
	}, nil
}

func (r *forgeRepository) PostInlineComment(
	ctx context.Context,
	id forge.ChangeID,
	req forge.InlineCommentRequest,
) (*forge.InlineComment, error) {
	u := r.apiURL.JoinPath(
		r.owner, r.repo, "inline-comments",
	)
	scope := req.Scope
	if scope == forge.CommentScopeUnknown {
		scope = forge.CommentScopeLine
	}
	body := postInlineCommentRequest{
		Change:   int(id.(ChangeID)),
		Scope:    scope.String(),
		Path:     req.Path,
		Line:     req.Line,
		Body:     req.Body,
		Side:     req.Side,
		ThreadID: req.ThreadID,
	}
	if req.Range != nil {
		body.RangeStart = req.Range.Start
		body.RangeEnd = req.Range.End
	}

	var res postInlineCommentResponse
	if err := r.client.Post(
		ctx, u.String(), body, &res,
	); err != nil {
		return nil, fmt.Errorf("post inline comment: %w", err)
	}

	out := &forge.InlineComment{
		ID:        ChangeCommentID(res.ID),
		ThreadID:  res.ThreadID,
		Scope:     scope,
		Path:      req.Path,
		Line:      req.Line,
		Side:      req.Side,
		Body:      req.Body,
		CreatedAt: res.CreatedAt,
	}
	if req.Range != nil {
		out.Range = &forge.CommentRange{
			Start: req.Range.Start,
			End:   req.Range.End,
		}
	}
	return out, nil
}

// Submit review (batch)

type submitReviewRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Change   int                          `json:"change"`
	Body     string                       `json:"body,omitempty"`
	Event    int                          `json:"event"`
	Comments []submitReviewCommentRequest `json:"comments"`
}

type submitReviewCommentRequest struct {
	Scope    string `json:"scope,omitempty"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Body     string `json:"body"`
	Side     string `json:"side,omitempty"`
	ThreadID string `json:"threadID,omitempty"`
}

type submitReviewResponse struct {
	CommentIDs []int `json:"commentIDs"`
}

func (sh *ShamHub) handleSubmitReview(
	_ context.Context,
	req *submitReviewRequest,
) (*submitReviewResponse, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	now := time.Now()
	var ids []int
	for _, c := range req.Comments {
		threadID := c.ThreadID
		if threadID == "" {
			threadID = fmt.Sprintf(
				"thread-%d", len(sh.comments)+1,
			)
		}

		scope := c.Scope
		if scope == "" {
			scope = "line"
		}
		comment := shamComment{
			ID:         len(sh.comments) + 1,
			Change:     req.Change,
			Body:       c.Body,
			Path:       c.Path,
			Line:       c.Line,
			Side:       c.Side,
			Scope:      scope,
			ThreadID:   threadID,
			Resolvable: true,
			Author:     "test-user",
			CreatedAt:  now,
		}
		sh.comments = append(sh.comments, comment)
		ids = append(ids, comment.ID)
	}

	return &submitReviewResponse{CommentIDs: ids}, nil
}

func (r *forgeRepository) SubmitReview(
	ctx context.Context,
	id forge.ChangeID,
	req forge.ReviewRequest,
) error {
	u := r.apiURL.JoinPath(r.owner, r.repo, "reviews")

	var comments []submitReviewCommentRequest
	for _, c := range req.Comments {
		scope := c.Scope
		if scope == forge.CommentScopeUnknown {
			scope = forge.CommentScopeLine
		}
		comments = append(comments, submitReviewCommentRequest{
			Scope:    scope.String(),
			Path:     c.Path,
			Line:     c.Line,
			Body:     c.Body,
			Side:     c.Side,
			ThreadID: c.ThreadID,
		})
	}

	body := submitReviewRequest{
		Change:   int(id.(ChangeID)),
		Body:     req.Body,
		Event:    int(req.Event),
		Comments: comments,
	}

	var res submitReviewResponse
	if err := r.client.Post(
		ctx, u.String(), body, &res,
	); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}

	return nil
}

// Resolve/unresolve threads

type resolveThreadRequest struct {
	Owner    string `path:"owner" json:"-"`
	Repo     string `path:"repo" json:"-"`
	ThreadID string `path:"threadID" json:"-"`
}

type resolveThreadResponse struct{}

func (sh *ShamHub) handleResolveThread(
	_ context.Context,
	req *resolveThreadRequest,
) (*resolveThreadResponse, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	found := false
	for i, c := range sh.comments {
		if c.ThreadID == req.ThreadID {
			sh.comments[i].Resolved = true
			found = true
		}
	}
	if !found {
		return nil, notFoundErrorf(
			"thread %s not found", req.ThreadID,
		)
	}

	return &resolveThreadResponse{}, nil
}

func (sh *ShamHub) handleUnresolveThread(
	_ context.Context,
	req *resolveThreadRequest,
) (*resolveThreadResponse, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	found := false
	for i, c := range sh.comments {
		if c.ThreadID == req.ThreadID {
			sh.comments[i].Resolved = false
			found = true
		}
	}
	if !found {
		return nil, notFoundErrorf(
			"thread %s not found", req.ThreadID,
		)
	}

	return &resolveThreadResponse{}, nil
}

func (r *forgeRepository) ResolveThread(
	ctx context.Context,
	threadID string,
) error {
	u := r.apiURL.JoinPath(
		r.owner, r.repo, "threads", threadID, "resolve",
	)

	var res resolveThreadResponse
	if err := r.client.Post(
		ctx, u.String(), struct{}{}, &res,
	); err != nil {
		return fmt.Errorf("resolve thread: %w", err)
	}

	return nil
}

func (r *forgeRepository) UnresolveThread(
	ctx context.Context,
	threadID string,
) error {
	u := r.apiURL.JoinPath(
		r.owner, r.repo, "threads", threadID, "unresolve",
	)

	var res resolveThreadResponse
	if err := r.client.Post(
		ctx, u.String(), struct{}{}, &res,
	); err != nil {
		return fmt.Errorf("unresolve thread: %w", err)
	}

	return nil
}

// Edit comment

type editCommentRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`
	ID    int    `path:"id" json:"-"`

	Body string `json:"body"`
}

type editCommentResponse struct {
	ID int `json:"id"`
}

func (sh *ShamHub) handleEditComment(
	_ context.Context,
	req *editCommentRequest,
) (*editCommentResponse, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, c := range sh.comments {
		if c.ID == req.ID {
			sh.comments[i].Body = req.Body
			return &editCommentResponse{ID: req.ID}, nil
		}
	}

	return nil, notFoundErrorf(
		"comment %d not found", req.ID,
	)
}

func (r *forgeRepository) EditComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	body string,
) error {
	cid := int(id.(ChangeCommentID))
	u := r.apiURL.JoinPath(
		r.owner, r.repo, "comments",
		strconv.Itoa(cid), "edit",
	)

	req := editCommentRequest{Body: body}
	var res editCommentResponse
	if err := r.client.Patch(
		ctx, u.String(), req, &res,
	); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}

	return nil
}
