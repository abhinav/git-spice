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

	Base      *string  `json:"base,omitempty"`
	Draft     *bool    `json:"draft,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Reviewers []string `json:"reviewers,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
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
	if len(req.Reviewers) > 0 {
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

		reviewers := sh.changes[changeIdx].RequestedReviewers
		for _, reviewer := range req.Reviewers {
			if !slices.Contains(reviewers, reviewer) {
				reviewers = append(reviewers, reviewer)
			}
		}
		sh.changes[changeIdx].RequestedReviewers = reviewers
	}

	if len(req.Assignees) > 0 {
		// Validate that all assignees are registered users.
		for _, assignee := range req.Assignees {
			found := false
			for _, u := range sh.users {
				if u.Username == assignee {
					found = true
					break
				}
			}
			if !found {
				return nil, badRequestErrorf("assignee %q is not a registered user", assignee)
			}
		}

		assignees := sh.changes[changeIdx].Assignees
		for _, assignee := range req.Assignees {
			if !slices.Contains(assignees, assignee) {
				assignees = append(assignees, assignee)
			}
		}
		sh.changes[changeIdx].Assignees = assignees
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
	req.Reviewers = opts.Reviewers
	req.Assignees = opts.Assignees

	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(r.owner, r.repo, "change", strconv.Itoa(int(id)))
	var res editChangeResponse
	if err := r.client.Patch(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("edit change: %w", err)
	}

	return nil
}
