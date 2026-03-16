package shamhub

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

type commentCountsRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	IDs []ChangeID `json:"ids"`
}

type commentCountsResponse struct {
	Counts []commentCountsItem `json:"counts"`
}

type commentCountsItem struct {
	Total      int `json:"total"`
	Resolved   int `json:"resolved"`
	Unresolved int `json:"unresolved"`
}

var _ = shamhubRESTHandler("POST /{owner}/{repo}/change/comment-counts", (*ShamHub).handleCommentCounts)

func (sh *ShamHub) handleCommentCounts(_ context.Context, req *commentCountsRequest) (*commentCountsResponse, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	counts := make([]commentCountsItem, len(req.IDs))
	for i, changeID := range req.IDs {
		counts[i] = sh.countCommentsForChange(int(changeID))
	}

	return &commentCountsResponse{Counts: counts}, nil
}

func (sh *ShamHub) countCommentsForChange(changeNum int) commentCountsItem {
	var total, resolved int
	for _, c := range sh.comments {
		if c.Change != changeNum || !c.Resolvable {
			continue
		}
		total++
		if c.Resolved {
			resolved++
		}
	}
	return commentCountsItem{
		Total:      total,
		Resolved:   resolved,
		Unresolved: total - resolved,
	}
}

// CommentCountsByChange retrieves comment resolution counts for multiple changes.
func (r *forgeRepository) CommentCountsByChange(
	ctx context.Context,
	fids []forge.ChangeID,
) ([]*forge.CommentCounts, error) {
	ids := make([]ChangeID, len(fids))
	for i, fid := range fids {
		ids[i] = fid.(ChangeID)
	}

	u := r.apiURL.JoinPath(r.owner, r.repo, "change", "comment-counts")
	req := commentCountsRequest{IDs: ids}

	var res commentCountsResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return nil, fmt.Errorf("get comment counts: %w", err)
	}

	counts := make([]*forge.CommentCounts, len(res.Counts))
	for i, c := range res.Counts {
		counts[i] = &forge.CommentCounts{
			Total:      c.Total,
			Resolved:   c.Resolved,
			Unresolved: c.Unresolved,
		}
	}

	return counts, nil
}
