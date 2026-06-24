package shamhub

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.abhg.dev/gs/internal/forge"
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
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	Resolved  bool      `json:"resolved"`
	Outdated  bool      `json:"outdated"`
	CreatedAt time.Time `json:"createdAt"`
}

func (sh *ShamHub) handleListInlineComments(
	_ context.Context,
	req *listInlineCommentsRequest,
) (*listInlineCommentsResponse, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	var items []inlineCommentItem
	for _, c := range sh.comments {
		if c.Change != req.Change || c.Path == "" {
			continue
		}
		items = append(items, inlineCommentItem{
			ID:        c.ID,
			ThreadID:  c.ThreadID,
			Path:      c.Path,
			Line:      c.Line,
			Body:      c.Body,
			Author:    c.Author,
			Resolved:  c.Resolved,
			Outdated:  false,
			CreatedAt: c.CreatedAt,
		})
	}

	return &listInlineCommentsResponse{Items: items}, nil
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
		comments = append(comments, &forge.InlineComment{
			ID:        ChangeCommentID(item.ID),
			ThreadID:  item.ThreadID,
			Path:      item.Path,
			Line:      item.Line,
			Body:      item.Body,
			Author:    item.Author,
			Resolved:  item.Resolved,
			Outdated:  item.Outdated,
			CreatedAt: item.CreatedAt,
		})
	}
	return comments, nil
}

// Post inline comment

type postInlineCommentRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Change   int    `json:"change"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Body     string `json:"body"`
	Side     string `json:"side"`
	ThreadID string `json:"threadID,omitempty"`
}

type postInlineCommentResponse struct {
	ID        int       `json:"id"`
	ThreadID  string    `json:"threadID"`
	CreatedAt time.Time `json:"createdAt"`
}

func (sh *ShamHub) handlePostInlineComment(
	_ context.Context,
	req *postInlineCommentRequest,
) (*postInlineCommentResponse, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	threadID := req.ThreadID
	if threadID == "" {
		// New thread: generate a thread ID.
		threadID = fmt.Sprintf("thread-%d", len(sh.comments)+1)
	}

	now := time.Now()
	comment := shamComment{
		ID:         len(sh.comments) + 1,
		Change:     req.Change,
		Body:       req.Body,
		Path:       req.Path,
		Line:       req.Line,
		Side:       req.Side,
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
	body := postInlineCommentRequest{
		Change:   int(id.(ChangeID)),
		Path:     req.Path,
		Line:     req.Line,
		Body:     req.Body,
		Side:     req.Side,
		ThreadID: req.ThreadID,
	}

	var res postInlineCommentResponse
	if err := r.client.Post(
		ctx, u.String(), body, &res,
	); err != nil {
		return nil, fmt.Errorf("post inline comment: %w", err)
	}

	return &forge.InlineComment{
		ID:        ChangeCommentID(res.ID),
		ThreadID:  res.ThreadID,
		Path:      req.Path,
		Line:      req.Line,
		Body:      req.Body,
		CreatedAt: res.CreatedAt,
	}, nil
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
	Path     string `json:"path"`
	Line     int    `json:"line"`
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

		comment := shamComment{
			ID:         len(sh.comments) + 1,
			Change:     req.Change,
			Body:       c.Body,
			Path:       c.Path,
			Line:       c.Line,
			Side:       c.Side,
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
		comments = append(comments, submitReviewCommentRequest{
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
