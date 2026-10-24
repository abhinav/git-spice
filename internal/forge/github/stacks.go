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
// GitHub requires native stacks to be linear.
// Only open pull requests with head branches in the receiving repository
// can join a stack.
// PlanStackUpdate preserves compatible existing native membership
// and selects the longest extension for each change tree.
// When divergence excludes a branch and its upstack from native membership,
// PlanStackUpdate warns once for that branch instead of returning an error.
//
// Planning is read-only.
// Executing the returned plan attempts each independent update
// and joins errors after all independent work has been attempted.
func (r *Repository) PlanStackUpdate(
	ctx context.Context,
	changes []forge.StackChange,
) (forge.StackUpdatePlan, error) {
	if !r.stacksEnabled {
		return nil, forge.ErrUnsupported
	}
	if err := r.gateway.CheckPullRequestStacks(ctx, r.owner, r.repo); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return nil, errors.Join(forge.ErrUnsupported, err)
		}
		return nil, fmt.Errorf("check GitHub native stack support: %w", err)
	}

	desired, err := newGitHubStackChanges(changes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	numbers := make([]int, len(desired))
	for i, change := range desired {
		numbers[i] = change.number
	}
	pullRequests, err := r.gateway.PullRequestsForStackUpdate(
		ctx,
		r.owner,
		r.repo,
		numbers,
	)
	if err != nil {
		return nil, fmt.Errorf("inspect GitHub pull requests for stack update: %w", err)
	}

	// The batch lookup preserves request order, including missing pull requests.
	// Keep the desired relationships and observed facts together for planning.
	for i, pullRequest := range pullRequests {
		desired[i].pullRequest = pullRequest
		if pullRequest != nil {
			desired[i].headInRepository = strings.EqualFold(pullRequest.HeadRepositoryOwner, r.owner) &&
				strings.EqualFold(pullRequest.HeadRepositoryName, r.repo)
		}
	}
	reconciliation := reconcileGitHubStacks(desired)
	for _, warning := range reconciliation.warnings {
		r.log.Warnf("%s", warning)
	}
	plan := &githubStackUpdatePlan{
		repository: r,
		program:    reconciliation.program,
		errs:       reconciliation.errs,
	}
	return plan, nil
}

// githubStackUpdatePlan owns the program compiled from one immutable snapshot.
// An attempted write may make the snapshot stale, so plans are single-use.
type githubStackUpdatePlan struct {
	repository *Repository
	program    githubStackProgram
	errs       []error
	executed   bool
}

// Execute applies every independent planned update and joins failures so
// one repository tree cannot prevent another from being reconciled.
func (p *githubStackUpdatePlan) Execute(ctx context.Context) error {
	if p.executed {
		return errors.New("GitHub stack update plan was already executed")
	}
	p.executed = true

	errs := p.errs
	if err := executeGitHubStackProgram(ctx, p.repository, p.program); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// githubStackChange pairs a desired relationship
// with the observed GitHub facts used to plan it.
// Reconciliation treats these records as immutable.
// A nil pullRequest means that GitHub did not find the requested change.
type githubStackChange struct {
	number int

	// baseNumber is the desired base PR in this request, or zero for a root.
	baseNumber int

	// baseBranch is the desired provider-facing base, not the observed base.
	baseBranch string

	pullRequest      *github.StackUpdatePullRequest
	headInRepository bool
}

func newGitHubStackChanges(
	changes []forge.StackChange,
) ([]githubStackChange, error) {
	changesByNumber := make(map[int]githubStackChange, len(changes))
	for _, change := range changes {
		number := mustPR(change.Change).Number
		if _, exists := changesByNumber[number]; exists {
			return nil, fmt.Errorf(
				"duplicate GitHub pull request #%d",
				number,
			)
		}
		changesByNumber[number] = githubStackChange{
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

	desired := make([]githubStackChange, len(orderedNumbers))
	for i, number := range orderedNumbers {
		desired[i] = changesByNumber[number]
	}
	return desired, nil
}
