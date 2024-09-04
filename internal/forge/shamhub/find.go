package shamhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
)

var (
	_ = shamhubHandler("GET /{owner}/{repo}/change/{number}", (*ShamHub).handleGetChange)
	_ = shamhubHandler("GET /{owner}/{repo}/changes/by-branch/{branch}", (*ShamHub).handleFindChangesByBranch)
)

func (sh *ShamHub) handleGetChange(w http.ResponseWriter, r *http.Request) {
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

	sh.mu.RLock()
	var (
		got   shamChange
		found bool
	)
	for _, c := range sh.changes {
		if c.Owner == owner && c.Repo == repo && c.Number == num {
			got = c
			found = true
			break
		}
	}
	sh.mu.RUnlock()

	if !found {
		http.Error(w, "change not found", http.StatusNotFound)
		return
	}

	change, err := sh.toChange(got)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(change); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (sh *ShamHub) handleFindChangesByBranch(w http.ResponseWriter, r *http.Request) {
	owner, repo, branch := r.PathValue("owner"), r.PathValue("repo"), r.PathValue("branch")
	if owner == "" || repo == "" || branch == "" {
		http.Error(w, "owner, repo, and branch are required", http.StatusBadRequest)
		return
	}

	limit := 10
	if l := r.FormValue("limit"); l != "" {
		var err error
		limit, err = strconv.Atoi(l)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	filters := []func(shamChange) bool{
		func(c shamChange) bool { return c.Owner == owner },
		func(c shamChange) bool { return c.Repo == repo },
		func(c shamChange) bool { return c.Head == branch },
	}

	if state := r.FormValue("state"); state != "" && state != "all" {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		changes[i] = change
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(changes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *forgeRepository) FindChangeByID(ctx context.Context, fid forge.ChangeID) (*forge.FindChangeItem, error) {
	id := fid.(ChangeID)
	u := f.apiURL.JoinPath(f.owner, f.repo, "change", strconv.Itoa(int(id)))
	var res Change
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
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

	return &forge.FindChangeItem{
		ID:       ChangeID(res.Number),
		URL:      res.URL,
		Subject:  res.Subject,
		HeadHash: git.Hash(res.Head.Hash),
		BaseName: res.Base.Name,
		Draft:    res.Draft,
		State:    state,
	}, nil
}

func (f *forgeRepository) FindChangesByBranch(ctx context.Context, branch string, opts forge.FindChangesOptions) ([]*forge.FindChangeItem, error) {
	if opts.Limit == 0 {
		opts.Limit = 10
	}

	u := f.apiURL.JoinPath(f.owner, f.repo, "changes", "by-branch", branch)
	q := u.Query()
	q.Set("limit", strconv.Itoa(opts.Limit))
	if opts.State == 0 {
		q.Set("state", "all")
	} else {
		q.Set("state", opts.State.String())
	}
	u.RawQuery = q.Encode()

	var res []*Change
	if err := f.client.Get(ctx, u.String(), &res); err != nil {
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

		changes[i] = &forge.FindChangeItem{
			ID:       ChangeID(c.Number),
			URL:      c.URL,
			State:    state,
			Subject:  c.Subject,
			HeadHash: git.Hash(c.Head.Hash),
			BaseName: c.Base.Name,
			Draft:    c.Draft,
		}
	}
	return changes, nil
}
