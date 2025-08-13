package shamhub

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

type editChangeRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	Base   *string  `json:"base,omitempty"`
	Draft  *bool    `json:"draft,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

type editChangeResponse struct{}

var _ = shamhubRESTHandler("PATCH /{owner}/{repo}/change/{number}", (*ShamHub).handleEditChange)

func (sh *ShamHub) handleEditChange(_ context.Context, req *editChangeRequest) (*editChangeResponse, error) {
	owner, repo, num := req.Owner, req.Repo, req.Number
	sh.mu.Lock()
	defer sh.mu.Unlock()

	changeIdx := -1
	for idx, change := range sh.changes {
		if change.Base.Owner == owner && change.Base.Repo == repo && change.Number == num {
			changeIdx = idx
			break
		}
	}
	if changeIdx == -1 {
		return nil, notFoundErrorf("change %s/%s#%d not found", owner, repo, num)
	}

	if b := req.Base; b != nil {
		sh.changes[changeIdx].Base.Name = *b
	}
	if d := req.Draft; d != nil {
		sh.changes[changeIdx].Draft = *d
	}
	if len(req.Labels) > 0 {
		labels := sh.changes[changeIdx].Labels
		for _, label := range req.Labels {
			if !slices.Contains(labels, label) {
				labels = append(labels, label)
			}
		}
		sh.changes[changeIdx].Labels = labels
	}

	return &editChangeResponse{}, nil // empty for now
}

func (r *forgeRepository) EditChange(ctx context.Context, fid forge.ChangeID, opts forge.EditChangeOptions) error {
	var req editChangeRequest
	if opts.Base != "" {
		req.Base = &opts.Base
	}
	if opts.Draft != nil {
		req.Draft = opts.Draft
	}
	req.Labels = opts.Labels

	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(r.owner, r.repo, "change", strconv.Itoa(int(id)))
	var res editChangeResponse
	if err := r.client.Patch(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("edit change: %w", err)
	}

	return nil
}
