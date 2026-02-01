package shamhub

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"slices"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeComment is a comment made on ShamHub.
type ChangeComment struct {
	ID     int
	Change int
	Body   string
}

// ListChangeComments returns all comments on all changes in ShamHub.
func (sh *ShamHub) ListChangeComments() ([]*ChangeComment, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	comments := make([]*ChangeComment, len(sh.comments))
	for i, c := range sh.comments {
		comments[i] = &ChangeComment{
			ID:     c.ID,
			Change: c.Change,
			Body:   c.Body,
		}
	}

	return comments, nil
}

// DeleteComment deletes a comment by its ID.
func (sh *ShamHub) DeleteComment(id int) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, c := range sh.comments {
		if c.ID == id {
			sh.comments = slices.Delete(sh.comments, i, i+1)
			return nil
		}
	}

	return fmt.Errorf("comment %d not found", id)
}

// ChangeCommentID uniquely identifies a comment on a change in ShamHub.
type ChangeCommentID int

var _ forge.ChangeCommentID = ChangeCommentID(0)

func (id ChangeCommentID) String() string {
	return strconv.Itoa(int(id))
}

type shamComment struct {
	ID     int
	Change int
	Body   string
}

var (
	_ = shamhubRESTHandler("POST /{owner}/{repo}/comments", (*ShamHub).handlePostChangeComment)
	_ = shamhubRESTHandler("PATCH /{owner}/{repo}/comments/{id}", (*ShamHub).handleUpdateChangeComment)
	_ = shamhubRESTHandler("DELETE /{owner}/{repo}/comments/{id}", (*ShamHub).handleDeleteChangeComment)
	_ = shamhubRESTHandler("GET /{owner}/{repo}/comments", (*ShamHub).handleListChangeComments)
)

type postCommentRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Change int    `json:"changeNumber,omitempty"`
	Body   string `json:"body,omitempty"`
}

type postCommentResponse struct {
	ID int `json:"id,omitempty"`
}

func (sh *ShamHub) handlePostChangeComment(_ context.Context, req *postCommentRequest) (*postCommentResponse, error) {
	owner, repo := req.Owner, req.Repo

	sh.mu.RLock()
	var found bool
	for _, c := range sh.changes {
		if c.Base.Owner == owner && c.Base.Repo == repo && c.Number == req.Change {
			found = true
			break
		}
	}
	sh.mu.RUnlock()

	if !found {
		return nil, notFoundErrorf("change %d not found in %s/%s", req.Change, owner, repo)
	}

	sh.mu.Lock()
	comment := shamComment{
		ID:     len(sh.comments) + 1,
		Change: req.Change,
		Body:   req.Body,
	}
	sh.comments = append(sh.comments, comment)
	sh.mu.Unlock()

	return &postCommentResponse{
		ID: comment.ID,
	}, nil
}

type updateCommentRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`
	ID    int    `path:"id" json:"-"`

	Body string `json:"body,omitempty"`
}

type updateCommentResponse struct {
	ID int `json:"id,omitempty"`
}

func (sh *ShamHub) handleUpdateChangeComment(_ context.Context, req *updateCommentRequest) (*updateCommentResponse, error) {
	// owner/repo not really used because comment IDs are globally unique.
	id := req.ID

	sh.mu.Lock()
	var found bool
	for i, c := range sh.comments {
		if c.ID == id {
			found = true
			sh.comments[i].Body = req.Body
			break
		}
	}
	sh.mu.Unlock()

	if !found {
		return nil, notFoundErrorf("comment %d not found in %s/%s", id, req.Owner, req.Repo)
	}

	return &updateCommentResponse{ID: id}, nil
}

func (r *forgeRepository) PostChangeComment(
	ctx context.Context,
	id forge.ChangeID,
	markdown string,
) (forge.ChangeCommentID, error) {
	u := r.apiURL.JoinPath(r.owner, r.repo, "comments")
	req := postCommentRequest{
		Change: int(id.(ChangeID)),
		Body:   markdown,
	}

	var res postCommentResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}

	return ChangeCommentID(res.ID), nil
}

func (r *forgeRepository) UpdateChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
	markdown string,
) error {
	cid := int(id.(ChangeCommentID))
	u := r.apiURL.JoinPath(r.owner, r.repo, "comments", strconv.Itoa(cid))
	req := updateCommentRequest{Body: markdown}
	var res updateCommentResponse
	if err := r.client.Patch(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("update comment: %w", err)
	}

	return nil
}

type deleteCommentRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`
	ID    int    `path:"id" json:"-"`
}

type deleteCommentResponse struct{}

func (sh *ShamHub) handleDeleteChangeComment(_ context.Context, req *deleteCommentRequest) (*deleteCommentResponse, error) {
	id := req.ID

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, c := range sh.comments {
		if c.ID == id {
			sh.comments = slices.Delete(sh.comments, i, i+1)
			return &deleteCommentResponse{}, nil
		}
	}

	return nil, notFoundErrorf("comment %d not found", id)
}

func (r *forgeRepository) DeleteChangeComment(
	ctx context.Context,
	id forge.ChangeCommentID,
) error {
	cid := int(id.(ChangeCommentID))
	u := r.apiURL.JoinPath(r.owner, r.repo, "comments", strconv.Itoa(cid))
	var res deleteCommentResponse
	if err := r.client.Delete(ctx, u.String(), &res); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

type listChangeCommentsRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Change int    `form:"change,required" json:"-"`
	Offset int    `form:"offset" json:"-"`
	Limit  int    `form:"limit" json:"-"`
}

type listChangeCommentsResponse struct {
	Items   []listChangeCommentsItem `json:"items,omitempty"`
	Offset  int                      `json:"offset,omitempty"`
	HasMore bool                     `json:"hasMore,omitempty"`
}

type listChangeCommentsItem struct {
	ID   int    `json:"id,omitempty"`
	Body string `json:"body,omitempty"`
}

func (sh *ShamHub) handleListChangeComments(_ context.Context, req *listChangeCommentsRequest) (*listChangeCommentsResponse, error) {
	// owner/repo not really used because change numbers are globally unique.
	changeNum, offset, limit := req.Change, req.Offset, cmp.Or(req.Limit, 10)

	sh.mu.RLock()
	var comments []shamComment
	for _, c := range sh.comments {
		if c.Change == changeNum {
			comments = append(comments, c)
		}
	}
	sh.mu.RUnlock()

	offset = min(offset, len(comments))      // bound the offset
	limit = min(limit, len(comments)-offset) // bound the limit

	var items []listChangeCommentsItem
	for _, c := range comments[offset : offset+limit] {
		items = append(items, listChangeCommentsItem{
			ID:   c.ID,
			Body: c.Body,
		})
	}

	return &listChangeCommentsResponse{
		Items:   items,
		Offset:  offset + limit,
		HasMore: offset+limit < len(comments),
	}, nil
}

var _listChangeCommentsPageSize = 10 // var for testing

func (r *forgeRepository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	opts *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	u := r.apiURL.JoinPath(r.owner, r.repo, "comments")
	q := u.Query()
	q.Set("change", strconv.Itoa(int(id.(ChangeID))))
	u.RawQuery = q.Encode()

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		offset := 0
		for {
			q.Set("offset", strconv.Itoa(offset))
			q.Set("limit", strconv.Itoa(_listChangeCommentsPageSize))
			u.RawQuery = q.Encode()

			var res listChangeCommentsResponse
			if err := r.client.Get(ctx, u.String(), &res); err != nil {
				yield(nil, err)
				return
			}

			for _, item := range res.Items {
				// Apply filtering if options are provided.
				if opts != nil {
					// Filter by BodyMatchesAll patterns.
					if len(opts.BodyMatchesAll) > 0 {
						matches := true
						for _, pattern := range opts.BodyMatchesAll {
							if !pattern.MatchString(item.Body) {
								matches = false
								break
							}
						}
						if !matches {
							continue
						}
					}
				}

				if !yield(&forge.ListChangeCommentItem{
					ID:   ChangeCommentID(item.ID),
					Body: item.Body,
				}, nil) {
					return
				}
			}

			if !res.HasMore {
				return
			}

			offset = res.Offset
		}
	}
}
