package shamhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go.abhg.dev/gs/internal/forge"
)

type submitChangeRequest struct {
	Subject string   `json:"subject,omitempty"`
	Body    string   `json:"body,omitempty"`
	Base    string   `json:"base,omitempty"`
	Head    string   `json:"head,omitempty"`
	Draft   bool     `json:"draft,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

type submitChangeResponse struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
}

var _ = shamhubHandler("POST /{owner}/{repo}/changes", (*ShamHub).handleSubmitChange)

func (sh *ShamHub) handleSubmitChange(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), r.PathValue("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo are required", http.StatusBadRequest)
		return
	}

	var data submitChangeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Reject requests where head or base haven't been pushed yet.
	ctx := r.Context()
	if !sh.branchRefExists(ctx, owner, repo, data.Base) {
		http.Error(w, "base branch does not exist", http.StatusBadRequest)
		return
	}
	if !sh.branchRefExists(ctx, owner, repo, data.Head) {
		http.Error(w, "head branch does not exist", http.StatusBadRequest)
		return
	}

	sh.mu.Lock()
	change := shamChange{
		// We'll just use a global counter for the change number for now.
		// We can scope it by owner/repo if needed.
		Number:  len(sh.changes) + 1,
		Owner:   owner,
		Repo:    repo,
		Draft:   data.Draft,
		Subject: data.Subject,
		Body:    data.Body,
		Base:    data.Base,
		Head:    data.Head,
		Labels:  data.Labels,
	}
	sh.changes = append(sh.changes, change)
	sh.mu.Unlock()

	res := submitChangeResponse{
		Number: change.Number,
		URL:    sh.changeURL(owner, repo, change.Number),
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *forgeRepository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	u := r.apiURL.JoinPath(r.owner, r.repo, "changes")
	var res submitChangeResponse
	if err := r.client.Post(ctx, u.String(), submitChangeRequest{
		Subject: req.Subject,
		Base:    req.Base,
		Body:    req.Body,
		Head:    req.Head,
		Draft:   req.Draft,
		Labels:  req.Labels,
	}, &res); err != nil {
		// Check if submit failed because base hasn't been pushed yet.
		if exists, err := r.RefExists(ctx, "refs/heads/"+req.Base); err == nil && !exists {
			return forge.SubmitChangeResult{}, errors.Join(forge.ErrUnsubmittedBase, err)
		} else if err != nil {
			r.log.Error("check base ref exists", "error", err)
		}

		return forge.SubmitChangeResult{}, fmt.Errorf("submit change: %w", err)
	}

	return forge.SubmitChangeResult{
		ID:  ChangeID(res.Number),
		URL: res.URL,
	}, nil
}
