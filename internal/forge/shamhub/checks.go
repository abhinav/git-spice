package shamhub

import (
	"context"
	"fmt"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// changeChecksRequest identifies the change whose checks state
// should be reported.
type changeChecksRequest struct {
	// Owner is the base repository owner.
	Owner string `path:"owner" json:"-"`

	// Repo is the base repository name.
	Repo string `path:"repo" json:"-"`

	// Number is the change number in the base repository.
	Number int `path:"number" json:"-"`
}

// changeChecksResponse is the wire response for aggregate checks state.
type changeChecksResponse struct {
	// State is one of pending, passed, or failed.
	State string `json:"state"`
}

var _ = shamhubRESTHandler(
	"GET /{owner}/{repo}/change/{number}/checks",
	(*ShamHub).handleChangeChecks,
)

func (sh *ShamHub) handleChangeChecks(
	_ context.Context, req changeChecksRequest,
) (*changeChecksResponse, error) {
	state, err := sh.ChangeChecksState(req.Owner, req.Repo, req.Number)
	if err != nil {
		return nil, err
	}
	return &changeChecksResponse{State: state.String()}, nil
}

// ChangeChecksState reports the aggregate checks state for a change.
func (sh *ShamHub) ChangeChecksState(
	owner string,
	repo string,
	number int,
) (forge.ChecksState, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	for _, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			if change.ChecksState == 0 {
				return forge.ChecksPassed, nil
			}
			return change.ChecksState, nil
		}
	}

	return 0, notFoundErrorf("change %d (%v/%v) not found", number, owner, repo)
}

// setStatusRequest updates the simulated checks state for a change.
type setStatusRequest struct {
	// Owner is the base repository owner.
	Owner string `path:"owner" json:"-"`

	// Repo is the base repository name.
	Repo string `path:"repo" json:"-"`

	// Number is the change number in the base repository.
	Number int `path:"number" json:"-"`

	// State is one of pending, passed, or failed.
	State string `json:"state"`
}

// setStatusResponse is empty because setting status has no result payload.
type setStatusResponse struct{}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/change/{number}/checks",
	(*ShamHub).handleSetStatus,
)

func (sh *ShamHub) handleSetStatus(
	_ context.Context, req setStatusRequest,
) (*setStatusResponse, error) {
	state, err := parseChecksState(req.State)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeChecksState(
		req.Owner, req.Repo, req.Number, state,
	); err != nil {
		return nil, err
	}
	return &setStatusResponse{}, nil
}

// SetChangeChecksState sets the aggregate checks state for a change.
func (sh *ShamHub) SetChangeChecksState(
	owner string,
	repo string,
	number int,
	state forge.ChecksState,
) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			sh.changes[i].ChecksState = state
			return nil
		}
	}

	return notFoundErrorf("change %d (%v/%v) not found", number, owner, repo)
}

// ChangeChecksState reports the aggregate checks state for a change.
func (r *forgeRepository) ChangeChecksState(
	ctx context.Context,
	fid forge.ChangeID,
) (forge.ChecksState, error) {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "checks",
	)

	var res changeChecksResponse
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return 0, fmt.Errorf("get checks: %w", err)
	}

	return parseChecksState(res.State)
}

func (r *forgeRepository) setChangeChecksState(
	ctx context.Context,
	fid forge.ChangeID,
	state forge.ChecksState,
) error {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "checks",
	)

	req := setStatusRequest{
		State: state.String(),
	}
	var res setStatusResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("set checks: %w", err)
	}
	return nil
}

func parseChecksState(value string) (forge.ChecksState, error) {
	switch value {
	case "pending":
		return forge.ChecksPending, nil
	case "passed":
		return forge.ChecksPassed, nil
	case "failed":
		return forge.ChecksFailed, nil
	case "none":
		return forge.ChecksNone, nil
	default:
		return 0, fmt.Errorf("unsupported status %q", value)
	}
}

// SetChangeChecks records the full per-change checks payload (rollup
// + runs + URL) for the given change. Used by tests to seed richer
// check state than [ShamHub.SetChangeChecksState] can.
func (sh *ShamHub) SetChangeChecks(
	owner, repo string,
	number int,
	checks *forge.ChangeChecks,
) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			if checks == nil {
				sh.changes[i].ChecksState = 0
				sh.changes[i].ChecksRuns = nil
				sh.changes[i].ChecksURL = ""
				return nil
			}
			sh.changes[i].ChecksState = checks.Rollup
			sh.changes[i].ChecksRuns = append(
				sh.changes[i].ChecksRuns[:0:0], checks.Runs...)
			sh.changes[i].ChecksURL = checks.URL
			return nil
		}
	}

	return notFoundErrorf("change %d (%v/%v) not found", number, owner, repo)
}

// ChecksByChange returns the per-change checks payload for each of
// the given change numbers, in the same order. Returns a nil slot for
// any unknown change.
func (sh *ShamHub) ChecksByChange(
	owner, repo string,
	numbers []int,
) ([]*forge.ChangeChecks, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	out := make([]*forge.ChangeChecks, len(numbers))
	for i, n := range numbers {
		for _, change := range sh.changes {
			if change.Base.Owner != owner ||
				change.Base.Repo != repo ||
				change.Number != n {
				continue
			}
			rollup := change.ChecksState
			if rollup == 0 {
				rollup = forge.ChecksNone
			}
			out[i] = &forge.ChangeChecks{
				Rollup: rollup,
				Runs:   append([]forge.CheckRun(nil), change.ChecksRuns...),
				URL:    change.ChecksURL,
			}
			break
		}
	}
	return out, nil
}

type checksByChangeRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	IDs []ChangeID `json:"ids"`
}

type checksByChangeResponse struct {
	Checks []*checksByChangeItem `json:"checks"`
}

type checksByChangeItem struct {
	Rollup string              `json:"rollup"`
	Runs   []checksByChangeRun `json:"runs,omitempty"`
	URL    string              `json:"url,omitempty"`
}

type checksByChangeRun struct {
	Name  string `json:"name"`
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/change/checks-by-change",
	(*ShamHub).handleChecksByChange,
)

func (sh *ShamHub) handleChecksByChange(
	_ context.Context, req *checksByChangeRequest,
) (*checksByChangeResponse, error) {
	numbers := make([]int, len(req.IDs))
	for i, id := range req.IDs {
		numbers[i] = int(id)
	}

	checks, err := sh.ChecksByChange(req.Owner, req.Repo, numbers)
	if err != nil {
		return nil, err
	}

	items := make([]*checksByChangeItem, len(checks))
	for i, c := range checks {
		if c == nil {
			continue
		}
		runs := make([]checksByChangeRun, len(c.Runs))
		for j, r := range c.Runs {
			runs[j] = checksByChangeRun{
				Name:  r.Name,
				State: r.State,
				URL:   r.URL,
			}
		}
		items[i] = &checksByChangeItem{
			Rollup: c.Rollup.String(),
			Runs:   runs,
			URL:    c.URL,
		}
	}
	return &checksByChangeResponse{Checks: items}, nil
}

// ChecksByChange retrieves per-change rolled-up and per-run check state.
func (r *forgeRepository) ChecksByChange(
	ctx context.Context, fids []forge.ChangeID,
) ([]*forge.ChangeChecks, error) {
	ids := make([]ChangeID, len(fids))
	for i, fid := range fids {
		ids[i] = fid.(ChangeID)
	}

	u := r.apiURL.JoinPath(r.owner, r.repo, "change", "checks-by-change")
	req := checksByChangeRequest{IDs: ids}

	var res checksByChangeResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return nil, fmt.Errorf("get checks: %w", err)
	}

	out := make([]*forge.ChangeChecks, len(res.Checks))
	for i, c := range res.Checks {
		if c == nil {
			continue
		}
		rollup, err := parseChecksState(c.Rollup)
		if err != nil {
			return nil, fmt.Errorf("checks[%d]: %w", i, err)
		}
		runs := make([]forge.CheckRun, len(c.Runs))
		for j, r := range c.Runs {
			runs[j] = forge.CheckRun{
				Name:  r.Name,
				State: r.State,
				URL:   r.URL,
			}
		}
		out[i] = &forge.ChangeChecks{
			Rollup: rollup,
			Runs:   runs,
			URL:    c.URL,
		}
	}
	return out, nil
}
