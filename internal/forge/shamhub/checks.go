package shamhub

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// changeChecksRequest identifies the change whose checks
// should be reported.
type changeChecksRequest struct {
	// Owner is the base repository owner.
	Owner string `path:"owner" json:"-"`

	// Repo is the base repository name.
	Repo string `path:"repo" json:"-"`

	// Number is the change number in the base repository.
	Number int `path:"number" json:"-"`
}

// changeChecksResponse is the wire response for change checks.
type changeChecksResponse struct {
	// Checks lists the checks reported for the change.
	Checks []changeCheckResponse `json:"checks,omitempty"`
}

// changeCheckResponse is one check in a changeChecksResponse.
type changeCheckResponse struct {
	// Name identifies the status check.
	Name string `json:"name"`

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
	checks, err := sh.ChangeChecks(req.Owner, req.Repo, req.Number)
	if err != nil {
		return nil, err
	}
	return &changeChecksResponse{Checks: checkResponses(checks)}, nil
}

// ChangeChecks reports checks for a change.
func (sh *ShamHub) ChangeChecks(
	owner string,
	repo string,
	number int,
) ([]forge.ChangeCheck, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	for _, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			return slices.Clone(change.Checks), nil
		}
	}

	return nil, notFoundErrorf("change %d (%v/%v) not found", number, owner, repo)
}

// setStatusRequest updates one simulated check for a change.
type setStatusRequest struct {
	// Owner is the base repository owner.
	Owner string `path:"owner" json:"-"`

	// Repo is the base repository name.
	Repo string `path:"repo" json:"-"`

	// Number is the change number in the base repository.
	Number int `path:"number" json:"-"`

	// Name identifies the check to update.
	Name string `json:"name"`

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
	if req.Name == "" {
		return nil, badRequestErrorf("check name is required")
	}
	state, err := parseChecksState(req.State)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeCheck(
		req.Owner, req.Repo, req.Number,
		forge.ChangeCheck{Name: req.Name, State: state},
	); err != nil {
		return nil, err
	}
	return &setStatusResponse{}, nil
}

// SetChangeCheck sets one named check for a change.
func (sh *ShamHub) SetChangeCheck(
	owner string,
	repo string,
	number int,
	check forge.ChangeCheck,
) error {
	if check.Name == "" {
		return errors.New("check name is required")
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			for j, existing := range sh.changes[i].Checks {
				if existing.Name == check.Name {
					sh.changes[i].Checks[j] = check
					return nil
				}
			}
			sh.changes[i].Checks = append(sh.changes[i].Checks, check)
			return nil
		}
	}

	return notFoundErrorf("change %d (%v/%v) not found", number, owner, repo)
}

// ChangeChecks reports checks for a change.
func (r *forgeRepository) ChangeChecks(
	ctx context.Context,
	fid forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "checks",
	)

	var res changeChecksResponse
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return nil, fmt.Errorf("get checks: %w", err)
	}

	checks := make([]forge.ChangeCheck, 0, len(res.Checks))
	for _, check := range res.Checks {
		state, err := parseChecksState(check.State)
		if err != nil {
			return nil, err
		}
		checks = append(checks, forge.ChangeCheck{
			Name:  check.Name,
			State: state,
		})
	}
	return checks, nil
}

func (r *forgeRepository) setChangeCheck(
	ctx context.Context,
	fid forge.ChangeID,
	check forge.ChangeCheck,
) error {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "checks",
	)

	req := setStatusRequest{
		Name:  check.Name,
		State: check.State.String(),
	}
	var res setStatusResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("set checks: %w", err)
	}
	return nil
}

func parseChecksState(value string) (forge.ChangeCheckState, error) {
	switch value {
	case "pending":
		return forge.ChangeCheckPending, nil
	case "passed":
		return forge.ChangeCheckPassed, nil
	case "failed":
		return forge.ChangeCheckFailed, nil
	default:
		return 0, fmt.Errorf("unsupported status %q", value)
	}
}

func checkResponses(checks []forge.ChangeCheck) []changeCheckResponse {
	responses := make([]changeCheckResponse, 0, len(checks))
	for _, check := range checks {
		responses = append(responses, changeCheckResponse{
			Name:  check.Name,
			State: check.State.String(),
		})
	}
	return responses
}
