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

// ChecksByChange reports per-change rolled-up and per-run check state
// for each of the given changes.
//
// TODO: real implementation lands on a follow-up branch.
// This stub returns one nil per id to satisfy the [forge.Repository]
// interface while the schema branch lands standalone.
func (r *forgeRepository) ChecksByChange(
	_ context.Context, ids []forge.ChangeID,
) ([]*forge.ChangeChecks, error) {
	return make([]*forge.ChangeChecks, len(ids)), nil
}
