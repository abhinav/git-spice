package github

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/graph"
)

var _ forge.StackRepository = (*Repository)(nil)

// PlanStackUpdate inspects the supplied change relationships and prepares the
// GitHub mutations needed to reconcile its native stack representation.
//
// GitHub accepts only linear stacks of open pull requests
// whose head branches belong to the receiving repository.
// PlanStackUpdate projects each unrestricted change tree
// onto that representation,
// preserves compatible existing native stack membership,
// and selects one longest remaining path.
// When divergence leaves a branch and its upstack out of the native stack,
// PlanStackUpdate warns once for the omitted branch tree
// instead of returning an error.
//
// Planning is read-only. Execute applies independent prepared transitions on a
// best-effort basis and joins genuine failures. Divergence only warns.
func (r *Repository) PlanStackUpdate(
	ctx context.Context,
	changes []forge.StackChange,
) (forge.StackUpdatePlan, error) {
	if err := r.gateway.CheckPullRequestStacks(ctx, r.owner, r.repo); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return nil, errors.Join(forge.ErrUnsupported, err)
		}
		return nil, fmt.Errorf("check GitHub native stack support: %w", err)
	}

	desired, err := newGitHubStackDesiredState(changes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pullRequests, err := r.gateway.PullRequestsForStackUpdate(
		ctx,
		r.owner,
		r.repo,
		desired.pullRequestNumbers(),
	)
	if err != nil {
		return nil, fmt.Errorf("inspect GitHub pull requests for stack update: %w", err)
	}

	reconciliation := reconcileGitHubStacks(
		desired.snapshot(r.owner, r.repo, pullRequests),
	)
	for _, warning := range reconciliation.warnings {
		r.log.Warnf("%s", warning)
	}
	plan := &githubStackUpdatePlan{
		repository:  r,
		transitions: reconciliation.transitions,
		errs:        reconciliation.errs,
	}
	return plan, nil
}

// githubStackUpdatePlan owns the transitions selected from one immutable
// snapshot. Plans are single-use because any attempted transition may make
// that snapshot stale.
type githubStackUpdatePlan struct {
	repository  *Repository
	transitions []githubStackTransition
	errs        []error
	executed    bool
}

// Execute applies every independent planned transition and joins failures so
// one repository tree cannot prevent another from being reconciled.
func (p *githubStackUpdatePlan) Execute(ctx context.Context) error {
	if p.executed {
		return errors.New("GitHub stack update plan was already executed")
	}
	p.executed = true

	errs := slices.Clone(p.errs)
	for _, transition := range p.transitions {
		if err := transition.execute(ctx, p.repository); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// githubStackDesiredState lists the desired pull request relationships and
// provider-facing bases in base-first order.
type githubStackDesiredState []githubStackDesiredChange

type githubStackDesiredChange struct {
	number     int
	baseNumber int
	baseBranch string
}

// githubStackSnapshot joins each desired change with the current GitHub state
// returned for it. A nil pull request means GitHub did not find that change.
type githubStackSnapshot []githubStackSnapshotChange

type githubStackSnapshotChange struct {
	desired     githubStackDesiredChange
	pullRequest *githubStackPullRequestState
}

type githubStackPullRequestState struct {
	id               github.ID
	state            github.PullRequestState
	baseBranch       string
	headInRepository bool
	stack            *github.PullRequestStack
}

func newGitHubStackDesiredState(
	changes []forge.StackChange,
) (githubStackDesiredState, error) {
	changesByNumber := make(map[int]githubStackDesiredChange, len(changes))
	for _, change := range changes {
		number := mustPR(change.Change).Number
		if _, exists := changesByNumber[number]; exists {
			return nil, fmt.Errorf(
				"duplicate GitHub pull request #%d",
				number,
			)
		}
		changesByNumber[number] = githubStackDesiredChange{
			number:     number,
			baseBranch: change.BaseBranch,
		}
	}
	for _, change := range changes {
		if change.BaseChange == nil {
			continue
		}
		number := mustPR(change.Change).Number
		baseNumber := mustPR(change.BaseChange).Number
		if _, exists := changesByNumber[baseNumber]; !exists {
			continue
		}
		desired := changesByNumber[number]
		desired.baseNumber = baseNumber
		changesByNumber[number] = desired
	}

	orderedNumbers, err := graph.Toposort(
		slices.Sorted(maps.Keys(changesByNumber)),
		func(number int) (int, bool) {
			baseNumber := changesByNumber[number].baseNumber
			return baseNumber, baseNumber != 0
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"order GitHub stack changes: %w",
			err,
		)
	}

	desired := make(githubStackDesiredState, len(orderedNumbers))
	for i, number := range orderedNumbers {
		desired[i] = changesByNumber[number]
	}
	return desired, nil
}

func (s githubStackDesiredState) pullRequestNumbers() []int {
	numbers := make([]int, len(s))
	for i, change := range s {
		numbers[i] = change.number
	}
	return numbers
}

// snapshot joins the current pull requests by position.
// PullRequestsForStackUpdate returns one entry for each requested number
// in the same order.
func (s githubStackDesiredState) snapshot(
	owner string,
	repo string,
	pullRequests []*github.StackUpdatePullRequest,
) githubStackSnapshot {
	observed := make(githubStackSnapshot, len(s))
	for i, desired := range s {
		pullRequest := pullRequests[i]
		observed[i].desired = desired
		if pullRequest == nil {
			continue
		}
		observed[i].pullRequest = &githubStackPullRequestState{
			id:         pullRequest.ID,
			state:      pullRequest.State,
			baseBranch: pullRequest.BaseRefName,
			headInRepository: strings.EqualFold(pullRequest.HeadRepositoryOwner, owner) &&
				strings.EqualFold(pullRequest.HeadRepositoryName, repo),
			stack: pullRequest.Stack,
		}
	}
	return observed
}
