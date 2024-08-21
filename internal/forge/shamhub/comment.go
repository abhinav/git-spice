package shamhub

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
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

// ChangeCommentID uniquely identifies a comment on a change in ShamHub.
type ChangeCommentID int

var _ forge.ChangeCommentID = ChangeCommentID(0)

func (id ChangeCommentID) String() string {
	return fmt.Sprintf("%d", int(id))
}

type shamComment struct {
	ID     int
	Change int
	Body   string
}

var (
	_ = shamhubHandler("GET /{owner}/{repo}/comments", (*ShamHub).handleListChangeComments)
	_ = shamhubHandler("POST /{owner}/{repo}/comments", (*ShamHub).handlePostChangeComment)
	_ = shamhubHandler("PATCH /{owner}/{repo}/comments/{id}", (*ShamHub).handleUpdateChangeComment)
)

type postCommentRequest struct {
	Change int    `json:"changeNumber,omitempty"`
	Body   string `json:"body,omitempty"`
}

type postCommentResponse struct {
	ID int `json:"id,omitempty"`
}

func (sh *ShamHub) handlePostChangeComment(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	var data postCommentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.RLock()
	var found bool
	for _, c := range sh.changes {
		if c.Owner == owner && c.Repo == repo && c.Number == data.Change {
			found = true
			break
		}
	}
	sh.mu.RUnlock()

	if !found {
		http.Error(w, "change not found", http.StatusNotFound)
		return
	}

	sh.mu.Lock()
	comment := shamComment{
		ID:     len(sh.comments) + 1,
		Change: data.Change,
		Body:   data.Body,
	}
	sh.comments = append(sh.comments, comment)
	sh.mu.Unlock()

	res := postCommentResponse{
		ID: int(comment.ID),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type updateCommentRequest struct {
	Body string `json:"body,omitempty"`
}

type updateCommentResponse struct {
	ID int `json:"id,omitempty"`
}

func (sh *ShamHub) handleUpdateChangeComment(w http.ResponseWriter, r *http.Request) {
	owner, repo, idStr := r.PathValue("owner"), r.PathValue("repo"), r.PathValue("id")
	if owner == "" || repo == "" || idStr == "" {
		http.Error(w, "owner, repo, and id are required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var data updateCommentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	var found bool
	for i, c := range sh.comments {
		if c.ID == id {
			found = true
			sh.comments[i].Body = data.Body
			break
		}
	}
	sh.mu.Unlock()

	if !found {
		http.Error(w, "comment not found", http.StatusNotFound)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(updateCommentResponse{ID: id}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

type listChangeCommentsResponse struct {
	Items   []listChangeCommentsItem `json:"items,omitempty"`
	Offset  int                      `json:"offset,omitempty"`
	HasMore bool                     `json:"hasMore,omitempty"`
}

type listChangeCommentsItem struct {
	ID   int    `json:"id,omitempty"`
	Body string `json:"body,omitempty"`
}

func (sh *ShamHub) handleListChangeComments(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	change, offsetstr, limitstr := r.FormValue("change"), r.FormValue("offset"), r.FormValue("limit")
	if change == "" {
		http.Error(w, "change is required", http.StatusBadRequest)
		return
	}

	changeNum, err := strconv.Atoi(change)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	offset := 0
	if offsetstr != "" {
		offset, err = strconv.Atoi(offsetstr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	limit := 10
	if limitstr != "" {
		limit, err = strconv.Atoi(limitstr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

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

	res := listChangeCommentsResponse{
		Items:   items,
		Offset:  offset + limit,
		HasMore: offset+limit < len(comments),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *forgeRepository) ListChangeComments(
	ctx context.Context,
	id forge.ChangeID,
	options *forge.ListChangeCommentsOptions,
) iter.Seq2[*forge.ListChangeCommentItem, error] {
	u := r.apiURL.JoinPath(r.owner, r.repo, "comments")
	q := u.Query()
	q.Set("change", strconv.Itoa(int(id.(ChangeID))))
	u.RawQuery = q.Encode()

	return func(yield func(*forge.ListChangeCommentItem, error) bool) {
		offset := 0
		for {
			q.Set("offset", strconv.Itoa(offset))
			q.Set("limit", "10")
			u.RawQuery = q.Encode()

			var res listChangeCommentsResponse
			if err := r.client.Get(ctx, u.String(), &res); err != nil {
				yield(nil, err)
				return
			}

			for _, item := range res.Items {
				yield(&forge.ListChangeCommentItem{
					ID:   ChangeCommentID(item.ID),
					Body: item.Body,
				}, nil)
			}

			if !res.HasMore {
				return
			}

			offset = res.Offset
		}
	}
}
