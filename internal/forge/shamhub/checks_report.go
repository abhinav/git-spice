package shamhub

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

// SetChangeChecks records the full per-change checks payload (rollup +
// per-run detail + URL) for the given change. Used by tests to seed
// richer check state than [ShamHub.SetChangeCheck] can.
//
// A nil report clears any previously seeded payload.
func (sh *ShamHub) SetChangeChecks(
	owner, repo string,
	number int,
	report *forge.ChecksReport,
) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			if report == nil {
				sh.changes[i].ChecksReport = nil
				return nil
			}
			clone := *report
			clone.Runs = append([]forge.CheckRun(nil), report.Runs...)
			sh.changes[i].ChecksReport = &clone
			return nil
		}
	}

	return notFoundErrorf("change %d (%v/%v) not found", number, owner, repo)
}

// ChecksByChange returns the per-change checks payload for each of the
// given change numbers, in the same order. A seeded-but-empty change
// reports [forge.ChecksRollupNone]; an unknown change yields a nil slot.
func (sh *ShamHub) ChecksByChange(
	owner, repo string,
	numbers []int,
) ([]*forge.ChecksReport, error) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	out := make([]*forge.ChecksReport, len(numbers))
	for i, n := range numbers {
		for _, change := range sh.changes {
			if change.Base.Owner != owner ||
				change.Base.Repo != repo ||
				change.Number != n {
				continue
			}
			if change.ChecksReport == nil {
				out[i] = &forge.ChecksReport{Rollup: forge.ChecksRollupNone}
			} else {
				clone := *change.ChecksReport
				clone.Runs = append(
					[]forge.CheckRun(nil), change.ChecksReport.Runs...)
				out[i] = &clone
			}
			break
		}
	}
	return out, nil
}

// checksByChangeRequest is the wire request for the batch checks API.
type checksByChangeRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	IDs []ChangeID `json:"ids"`
}

// checksByChangeResponse is the wire response for the batch checks API.
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

	reports, err := sh.ChecksByChange(req.Owner, req.Repo, numbers)
	if err != nil {
		return nil, err
	}

	items := make([]*checksByChangeItem, len(reports))
	for i, c := range reports {
		if c == nil {
			continue
		}
		runs := make([]checksByChangeRun, len(c.Runs))
		for j, r := range c.Runs {
			runs[j] = checksByChangeRun{Name: r.Name, State: r.State, URL: r.URL}
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
) ([]*forge.ChecksReport, error) {
	ids := make([]ChangeID, len(fids))
	for i, fid := range fids {
		ids[i] = fid.(ChangeID)
	}

	u := r.apiURL.JoinPath(r.owner, r.repo, "change", "checks-by-change")
	req := checksByChangeRequest{IDs: ids}

	var res checksByChangeResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return nil, fmt.Errorf("get checks by change: %w", err)
	}

	out := make([]*forge.ChecksReport, len(res.Checks))
	for i, c := range res.Checks {
		if c == nil {
			continue
		}

		var rollup forge.ChecksRollupState
		if err := rollup.UnmarshalText([]byte(c.Rollup)); err != nil {
			return nil, fmt.Errorf("checks[%d]: %w", i, err)
		}

		runs := make([]forge.CheckRun, len(c.Runs))
		for j, rn := range c.Runs {
			runs[j] = forge.CheckRun{Name: rn.Name, State: rn.State, URL: rn.URL}
		}
		out[i] = &forge.ChecksReport{Rollup: rollup, Runs: runs, URL: c.URL}
	}
	return out, nil
}
