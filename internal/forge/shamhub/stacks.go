package shamhub

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
)

type stackChange struct {
	Number     int    `json:"number"`
	Base       int    `json:"base,omitempty"`
	BaseBranch string `json:"base_branch,omitempty"`
}

type updateStackRequest struct {
	Owner string `path:"owner" json:"-"`
	Repo  string `path:"repo" json:"-"`

	Changes []stackChange `json:"changes"`
}

type updateStackResponse struct{}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/stack/update",
	(*ShamHub).handleUpdateStack,
)

func (sh *ShamHub) handleUpdateStack(
	_ context.Context,
	req *updateStackRequest,
) (*updateStackResponse, error) {
	if err := sh.updateStack(req.Owner, req.Repo, req.Changes); err != nil {
		return nil, err
	}
	return &updateStackResponse{}, nil
}

// updateStack atomically replaces every stored native-stack component that
// intersects the request. Validation finishes before existing relationships
// are changed.
func (sh *ShamHub) updateStack(
	owner string,
	repo string,
	changes []stackChange,
) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Resolve and validate the complete request before altering stack state.
	changesByNumber := make(map[int]*shamChange, len(changes))
	for i := range sh.changes {
		change := &sh.changes[i]
		if change.Base.Owner == owner && change.Base.Repo == repo {
			changesByNumber[change.Number] = change
		}
	}
	for _, member := range changes {
		change := changesByNumber[member.Number]
		if change == nil {
			return fmt.Errorf("change %d (%s/%s) not found", member.Number, owner, repo)
		}
		if change.State != shamChangeOpen {
			return fmt.Errorf("change %d is not open", member.Number)
		}
		if member.Base == 0 {
			continue
		}

		base := changesByNumber[member.Base]
		if base == nil {
			return fmt.Errorf("base change %d not found", member.Base)
		}
		if member.BaseBranch != base.Head.Name {
			return fmt.Errorf(
				"change %d desired base %q does not match change %d head %q",
				member.Number, member.BaseBranch, member.Base, base.Head.Name,
			)
		}
	}

	repository := repoID{Owner: owner, Name: repo}
	baseByChange := sh.stackBases[repository]
	if baseByChange == nil {
		baseByChange = make(map[int]int)
		sh.stackBases[repository] = baseByChange
	}

	// Discover each complete stored component touched by the request. Walking
	// both downstack and upstack ensures that replacement also removes old
	// divergent paths omitted from the new representation.
	touched := make(map[int]struct{}, len(changes))
	for _, change := range changes {
		touched[change.Number] = struct{}{}
	}
	for changed := true; changed; {
		changed = false
		for above, below := range baseByChange {
			_, aboveTouched := touched[above]
			_, belowTouched := touched[below]
			if !aboveTouched && !belowTouched {
				continue
			}
			if !aboveTouched {
				touched[above] = struct{}{}
				changed = true
			}
			if below != 0 && !belowTouched {
				touched[below] = struct{}{}
				changed = true
			}
		}
	}

	// Replace the touched components only after their full extent is known.
	for number := range touched {
		delete(baseByChange, number)
	}
	for _, change := range changes {
		baseByChange[change.Number] = change.Base
		changesByNumber[change.Number].Base.Name = change.BaseBranch
	}
	return nil
}

var _ forge.StackRepository = (*stackRepository)(nil)

// PlanStackUpdate prepares an atomic replacement of ShamHub's native stack
// relationships and provider-facing change bases.
func (r *stackRepository) PlanStackUpdate(
	_ context.Context,
	changes []forge.StackChange,
) (forge.StackUpdatePlan, error) {
	requestedChanges := make(map[ChangeID]struct{}, len(changes))
	for _, change := range changes {
		requestedChanges[change.Change.(ChangeID)] = struct{}{}
	}

	req := updateStackRequest{
		Changes: make([]stackChange, len(changes)),
	}
	// The forge contract treats a base outside the request as a root. Preserve
	// that distinction at the transport boundary instead of asking the server
	// to reconstruct caller intent.
	for i, change := range changes {
		req.Changes[i].Number = int(change.Change.(ChangeID))
		req.Changes[i].BaseBranch = change.BaseBranch
		base, ok := change.BaseChange.(ChangeID)
		if !ok {
			continue
		}
		if _, ok := requestedChanges[base]; ok {
			req.Changes[i].Base = int(base)
		}
	}

	return &shamHubStackUpdatePlan{repository: r, request: req}, nil
}

type shamHubStackUpdatePlan struct {
	repository *stackRepository
	request    updateStackRequest
	executed   bool
}

func (p *shamHubStackUpdatePlan) Execute(ctx context.Context) error {
	if p.executed {
		return errors.New("ShamHub stack update plan was already executed")
	}
	p.executed = true

	var res updateStackResponse
	if err := p.repository.client.Post(
		ctx,
		p.repository.apiURL.JoinPath(
			p.repository.owner,
			p.repository.repo,
			"stack",
			"update",
		).String(),
		p.request,
		&res,
	); err != nil {
		return fmt.Errorf("execute stack update: %w", err)
	}
	return nil
}
