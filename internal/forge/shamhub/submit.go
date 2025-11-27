package shamhub

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
)

type submitChangeRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Subject   string   `json:"subject,omitempty"`
	Body      string   `json:"body,omitempty"`
	Base      string   `json:"base,omitempty"`
	Head      string   `json:"head,omitempty"`
	HeadRepo  string   `json:"head_repo,omitempty"` // Format: "owner/repo", if different from target
	Draft     bool     `json:"draft,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
}

type submitChangeResponse struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url,omitempty"`
}

var _ = shamhubRESTHandler("POST /{owner}/{repo}/changes", (*ShamHub).handleSubmitChange)

func (sh *ShamHub) handleSubmitChange(ctx context.Context, req *submitChangeRequest) (*submitChangeResponse, error) {
	owner, repo := req.Owner, req.Repo

	// Reject requests where head or base haven't been pushed yet.
	if !sh.branchRefExists(ctx, owner, repo, req.Base) {
		return nil, badRequestErrorf("base branch does not exist")
	}

	// Check head branch - might be in a different repository (fork)
	headOwner, headRepo := owner, repo
	if req.HeadRepo != "" {
		var ok bool
		headOwner, headRepo, ok = strings.Cut(req.HeadRepo, "/")
		if !ok {
			return nil, badRequestErrorf("invalid head_repo format, expected 'owner/repo'")
		}
	}
	if !sh.branchRefExists(ctx, headOwner, headRepo, req.Head) {
		return nil, badRequestErrorf("head branch does not exist")
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Validate that all requested reviewers are registered users.
	for _, reviewer := range req.Reviewers {
		found := false
		for _, u := range sh.users {
			if u.Username == reviewer {
				found = true
				break
			}
		}
		if !found {
			return nil, badRequestErrorf("reviewer %q is not a registered user", reviewer)
		}
	}

	change := shamChange{
		// We'll just use a global counter for the change number for now.
		// We can scope it by owner/repo if needed.
		Number:             len(sh.changes) + 1,
		Draft:              req.Draft,
		Subject:            req.Subject,
		Body:               req.Body,
		Base:               &shamBranch{Owner: owner, Repo: repo, Name: req.Base},
		Head:               &shamBranch{Owner: headOwner, Repo: headRepo, Name: req.Head},
		Labels:             req.Labels,
		RequestedReviewers: req.Reviewers,
	}
	sh.changes = append(sh.changes, change)

	return &submitChangeResponse{
		Number: change.Number,
		URL:    sh.changeURL(owner, repo, change.Number),
	}, nil
}

func (r *forgeRepository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	u := r.apiURL.JoinPath(r.owner, r.repo, "changes")

	submitReq := submitChangeRequest{
		Subject:   req.Subject,
		Base:      req.Base,
		Body:      req.Body,
		Head:      req.Head,
		Draft:     req.Draft,
		Labels:    req.Labels,
		Reviewers: req.Reviewers,
	}

	// For now, fork functionality is handled at the ShamHub server level
	// Future enhancement: detect if head branch is from a fork

	var res submitChangeResponse
	if err := r.client.Post(ctx, u.String(), submitReq, &res); err != nil {
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
