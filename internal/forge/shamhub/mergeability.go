package shamhub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/xec"
)

type changeMergeabilityRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`
}

type changeMergeabilityResponse struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

var _ = shamhubRESTHandler(
	"GET /{owner}/{repo}/change/{number}/mergeability",
	(*ShamHub).handleChangeMergeability,
)

func (sh *ShamHub) handleChangeMergeability(
	_ context.Context,
	req changeMergeabilityRequest,
) (*changeMergeabilityResponse, error) {
	mergeability, err := sh.ChangeMergeability(req.Owner, req.Repo, req.Number)
	if err != nil {
		return nil, err
	}
	return &changeMergeabilityResponse{
		State:  mergeability.State.String(),
		Reason: mergeability.Reason.String(),
	}, nil
}

// SetChangeMergeabilityRequest requests a simulated mergeability result
// for one ShamHub change.
type SetChangeMergeabilityRequest struct {
	// Owner is the base repository owner.
	Owner string

	// Repo is the base repository name.
	Repo string

	// Number is the change number in the base repository.
	Number int

	// Mergeability is the result ShamHub should report for the change.
	Mergeability forge.ChangeMergeability
}

// SetChangeMergeability sets a simulated mergeability result for a change.
func (sh *ShamHub) SetChangeMergeability(req SetChangeMergeabilityRequest) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for i, change := range sh.changes {
		if change.Base.Owner == req.Owner &&
			change.Base.Repo == req.Repo &&
			change.Number == req.Number {
			sh.changes[i].Mergeability = &req.Mergeability
			return nil
		}
	}

	return notFoundErrorf(
		"change %d (%v/%v) not found",
		req.Number, req.Owner, req.Repo,
	)
}

type setMergeabilityRequest struct {
	Owner  string `path:"owner" json:"-"`
	Repo   string `path:"repo" json:"-"`
	Number int    `path:"number" json:"-"`

	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type setMergeabilityResponse struct{}

var _ = shamhubRESTHandler(
	"POST /{owner}/{repo}/change/{number}/mergeability",
	(*ShamHub).handleSetMergeability,
)

func (sh *ShamHub) handleSetMergeability(
	_ context.Context,
	req setMergeabilityRequest,
) (*setMergeabilityResponse, error) {
	mergeability, err := parseMergeability(req.State, req.Reason)
	if err != nil {
		return nil, badRequestErrorf("%s", err)
	}
	if err := sh.SetChangeMergeability(SetChangeMergeabilityRequest{
		Owner:        req.Owner,
		Repo:         req.Repo,
		Number:       req.Number,
		Mergeability: mergeability,
	}); err != nil {
		return nil, err
	}
	return &setMergeabilityResponse{}, nil
}

// ChangeMergeability reports whether ShamHub can merge the change.
func (sh *ShamHub) ChangeMergeability(
	owner string,
	repo string,
	number int,
) (forge.ChangeMergeability, error) {
	change, ok := sh.findChange(owner, repo, number)
	if !ok {
		return forge.ChangeMergeability{}, notFoundErrorf(
			"change %d (%v/%v) not found",
			number, owner, repo,
		)
	}
	if change.Mergeability != nil {
		return *change.Mergeability, nil
	}
	if change.State != shamChangeOpen {
		return forge.ChangeMergeability{
			State: forge.ChangeMergeabilityBlocked,
		}, nil
	}
	if change.Draft {
		return forge.ChangeMergeability{
			State:  forge.ChangeMergeabilityBlocked,
			Reason: forge.ChangeMergeabilityReasonDraft,
		}, nil
	}

	if err := sh.canMerge(change); err != nil {
		var mergeTreeErr mergeTreeError
		if errors.As(err, &mergeTreeErr) && mergeTreeErr.Conflict() {
			return forge.ChangeMergeability{
				State:  forge.ChangeMergeabilityBlocked,
				Reason: forge.ChangeMergeabilityReasonConflicts,
			}, nil
		}
		return forge.ChangeMergeability{}, fmt.Errorf("check mergeability: %w", err)
	}

	return forge.ChangeMergeability{
		State: forge.ChangeMergeabilityReady,
	}, nil
}

func (sh *ShamHub) findChange(owner, repo string, number int) (shamChange, bool) {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	for _, change := range sh.changes {
		if change.Base.Owner == owner &&
			change.Base.Repo == repo &&
			change.Number == number {
			return change, true
		}
	}
	return shamChange{}, false
}

func (sh *ShamHub) canMerge(change shamChange) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	targetRepoDir := sh.repoDir(change.Base.Owner, change.Base.Repo)
	if !sh.branchRefExists(
		ctx, change.Base.Owner, change.Base.Repo, change.Base.Name,
	) {
		return fmt.Errorf("base branch %q does not exist", change.Base.Name)
	}

	headRef := change.Head.Name
	if change.Head.Owner != change.Base.Owner ||
		change.Head.Repo != change.Base.Repo {
		if !sh.branchRefExists(
			ctx, change.Head.Owner, change.Head.Repo, change.Head.Name,
		) {
			return fmt.Errorf("head branch %q does not exist", change.Head.Name)
		}

		forkRepoDir := sh.repoDir(change.Head.Owner, change.Head.Repo)
		headRef = "refs/shamhub/mergeability/" +
			strconv.Itoa(change.Number) + "/head"
		if err := xec.Command(
			ctx, sh.log, sh.gitExe, "update-ref", "-d", headRef,
		).WithDir(targetRepoDir).Run(); err != nil {
			return fmt.Errorf("delete cached head branch: %w", err)
		}
		if err := xec.Command(
			ctx, sh.log, sh.gitExe,
			"fetch", forkRepoDir, change.Head.Name+":"+headRef,
		).WithDir(targetRepoDir).Run(); err != nil {
			return fmt.Errorf("fetch head branch: %w", err)
		}
	} else if !sh.branchRefExists(
		ctx, change.Head.Owner, change.Head.Repo, change.Head.Name,
	) {
		return fmt.Errorf("head branch %q does not exist", change.Head.Name)
	}

	if err := xec.Command(
		ctx, sh.log, sh.gitExe,
		"merge-tree", "--write-tree", change.Base.Name, headRef,
	).WithDir(targetRepoDir).Run(); err != nil {
		return mergeTreeError{err: err}
	}
	return nil
}

type mergeTreeError struct {
	err error
}

func (e mergeTreeError) Error() string {
	return "merge-tree: " + e.err.Error()
}

func (e mergeTreeError) Unwrap() error {
	return e.err
}

func (e mergeTreeError) Conflict() bool {
	var exitErr *xec.ExitError
	return errors.As(e.err, &exitErr) && exitErr.ExitCode() == 1
}

// ChangeMergeability reports whether the change can be merged.
func (r *forgeRepository) ChangeMergeability(
	ctx context.Context,
	fid forge.ChangeID,
) (forge.ChangeMergeability, error) {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "mergeability",
	)

	var res changeMergeabilityResponse
	if err := r.client.Get(ctx, u.String(), &res); err != nil {
		return forge.ChangeMergeability{}, fmt.Errorf("get mergeability: %w", err)
	}
	return parseMergeability(res.State, res.Reason)
}

func (r *forgeRepository) setChangeMergeability(
	ctx context.Context,
	fid forge.ChangeID,
	mergeability forge.ChangeMergeability,
) error {
	id := fid.(ChangeID)
	u := r.apiURL.JoinPath(
		r.owner, r.repo,
		"change", strconv.Itoa(int(id)), "mergeability",
	)

	req := setMergeabilityRequest{
		State:  mergeability.State.String(),
		Reason: mergeability.Reason.String(),
	}
	var res setMergeabilityResponse
	if err := r.client.Post(ctx, u.String(), req, &res); err != nil {
		return fmt.Errorf("set mergeability: %w", err)
	}
	return nil
}

func parseMergeability(state, reason string) (forge.ChangeMergeability, error) {
	mergeabilityState, err := parseMergeabilityState(state)
	if err != nil {
		return forge.ChangeMergeability{}, err
	}
	mergeabilityReason, err := parseMergeabilityReason(reason)
	if err != nil {
		return forge.ChangeMergeability{}, err
	}
	return forge.ChangeMergeability{
		State:  mergeabilityState,
		Reason: mergeabilityReason,
	}, nil
}

func parseMergeabilityState(value string) (forge.ChangeMergeabilityState, error) {
	switch strings.ToLower(value) {
	case "", "unknown":
		return forge.ChangeMergeabilityUnknown, nil
	case "ready":
		return forge.ChangeMergeabilityReady, nil
	case "waiting":
		return forge.ChangeMergeabilityWaiting, nil
	case "blocked":
		return forge.ChangeMergeabilityBlocked, nil
	default:
		return 0, fmt.Errorf("unsupported mergeability state %q", value)
	}
}

func parseMergeabilityReason(value string) (forge.ChangeMergeabilityReason, error) {
	switch strings.ToLower(value) {
	case "", "unknown":
		return forge.ChangeMergeabilityReasonUnknown, nil
	case "checks":
		return forge.ChangeMergeabilityReasonChecks, nil
	case "review":
		return forge.ChangeMergeabilityReasonReview, nil
	case "draft":
		return forge.ChangeMergeabilityReasonDraft, nil
	case "conflicts":
		return forge.ChangeMergeabilityReasonConflicts, nil
	case "behind":
		return forge.ChangeMergeabilityReasonBehind, nil
	case "discussions":
		return forge.ChangeMergeabilityReasonDiscussions, nil
	case "policy":
		return forge.ChangeMergeabilityReasonPolicy, nil
	default:
		return 0, fmt.Errorf("unsupported mergeability reason %q", value)
	}
}
