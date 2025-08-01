package shamhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

type editChangeRequest struct {
	Base   *string  `json:"base,omitempty"`
	Draft  *bool    `json:"draft,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

type editChangeResponse struct{}

var _ = shamhubHandler("PATCH /{owner}/{repo}/change/{number}", (*ShamHub).handleEditChange)

func (sh *ShamHub) handleEditChange(w http.ResponseWriter, r *http.Request) {
	owner, repo, numStr := r.PathValue("owner"), r.PathValue("repo"), r.PathValue("number")
	if owner == "" || repo == "" || numStr == "" {
		http.Error(w, "owner, repo, and number are required", http.StatusBadRequest)
		return
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var data editChangeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	changeIdx := -1
	for idx, change := range sh.changes {
		if change.Owner == owner && change.Repo == repo && change.Number == num {
			changeIdx = idx
			break
		}
	}
	if changeIdx == -1 {
		http.Error(w, "change not found", http.StatusNotFound)
		return
	}

	if b := data.Base; b != nil {
		sh.changes[changeIdx].Base = *b
	}
	if d := data.Draft; d != nil {
		sh.changes[changeIdx].Draft = *d
	}
	if len(data.Labels) > 0 {
		labels := sh.changes[changeIdx].Labels
		for _, label := range data.Labels {
			if !slices.Contains(labels, label) {
				labels = append(labels, label)
			}
		}
		sh.changes[changeIdx].Labels = labels
	}

	res := editChangeResponse{} // empty for now

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
