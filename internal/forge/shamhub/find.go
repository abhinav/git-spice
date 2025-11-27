package shamhub

import (
	"cmp"
	"context"
	"fmt"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

var (
	_ = shamhubRESTHandler("GET /{owner}/{repo}/change/{number}", (*ShamHub).handleGetChange)
	_ = shamhubRESTHandler("GET /{owner}/{repo}/changes/by-branch/{branch...}", (*ShamHub).handleFindChangesByBranch)
)

type getChangeRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`
}

func (sh *ShamHub) handleGetChange(_ context.Context, req *getChangeRequest) (*Change, error) {
	owner, repo, num := req.Owner, req.Repo, req.Number
	sh.mu.RLock()
	var (
		got   shamChange
		found bool
	)
	for _, c := range sh.changes {
		if c.Base.Owner == owner && c.Base.Repo == repo && c.Number == num {
			got = c
			found = true
			break
		}
	}
	sh.mu.RUnlock()

	if !found {
		return nil, notFoundErrorf("change %s/%s#%d not found", owner, repo, num)
	}

	return sh.toChange(got)
}

type findChangesByBranchRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Branch string `path:"branch" json:"-"`

	Limit int    `form:"limit" json:"-"`
	State string `form:"state" json:"-"`
}

func (sh *ShamHub) handleFindChangesByBranch(_ context.Context, req *findChangesByBranchRequest) ([]*Change, error) {
	owner, repo, branch := req.Owner, req.Repo, req.Branch

	limit := cmp.Or(req.Limit, 10) // default limit is 10
	filters := []func(shamChange) bool{
		func(c shamChange) bool { return c.Base.Owner == owner },
		func(c shamChange) bool { return c.Base.Repo == repo },
		func(c shamChange) bool { return c.Head.Name == branch },
	}

	if state := req.State; state != "" && state != "all" {
		var s shamChangeState
		switch state {
		case "open":
			s = shamChangeOpen
		case "closed":
			s = shamChangeClosed
		case "merged":
			s = shamChangeMerged
		}

		filters = append(filters, func(c shamChange) bool { return c.State == s })
	}

	var got []shamChange
	sh.mu.RLock()
nextChange:
	for _, c := range sh.changes {
		if len(got) >= limit {
			break
		}

		for _, f := range filters {
			if !f(c) {
				continue nextChange
			}
		}

		got = append(got, c)
	}
	sh.mu.RUnlock()

	changes := make([]*Change, len(got))
	for i, c := range got {
		change, err := sh.toChange(c)
		if err != nil {
			return nil, fmt.Errorf("convert shamChange to Change: %w", err)
		}

		changes[i] = change
	}

	return changes, nil
}

func (r *forgeRepository) FindChangeByID(ctx context.Context, fid forge.ChangeID) (*forge.FindChangeItem, error) {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(r.owner, r.repo, "change", strconv.Itoa(int(id)))
	var res Change
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("find change by ID: %w", err)
	}

	var state forge.ChangeState
	switch res.State {
	case "open":
		state = forge.ChangeOpen
	case "closed":
		if res.Merged {
			state = forge.ChangeMerged
		} else {
			state = forge.ChangeClosed
		}
	}

	labels := res.Labels
	if len(labels) == 0 {
		labels = nil
	}

	return &forge.FindChangeItem{
		ID:       ChangeID(res.Number),
		URL:      res.URL,
		Subject:  res.Subject,
		HeadHash: git.Hash(res.Head.Hash),
		BaseName: res.Base.Name,
		Draft:    res.Draft,
		State:    state,
		Labels:   labels,
	}, nil
}

func (r *forgeRepository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	u := r.apiURL.JoinPath(r.owner, r.repo, "changes", "by-branch", branch)
	q := u.Query()
	q.Set("limit", strconv.Itoa(opts.Limit))
	if opts.State == 0 {
		q.Set("state", "all")
	} else {
		q.Set("state", opts.State.String())
	}
	u.RawQuery = q.Encode()

	var res []*Change
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("find changes by branch: %w", err)
	}

	changes := make([]*forge.FindChangeItem, len(res))
	for i, c := range res {
		var state forge.ChangeState
		switch c.State {
		case "open":
			state = forge.ChangeOpen
		case "closed":
			if c.Merged {
				state = forge.ChangeMerged
			} else {
				state = forge.ChangeClosed
			}
		}

		labels := c.Labels
		if len(labels) == 0 {
			labels = nil
		}

		changes[i] = &forge.FindChangeItem{
			ID:       ChangeID(c.Number),
			URL:      c.URL,
			State:    state,
			Subject:  c.Subject,
			HeadHash: git.Hash(c.Head.Hash),
			BaseName: c.Base.Name,
			Draft:    c.Draft,
			Labels:   labels,
		}
	}
	return changes, nil
}
