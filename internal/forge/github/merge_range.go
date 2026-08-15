package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

// PlanMergeRanges identifies atomic merge ranges from GitHub's current native
// stack membership.
// Unstacked pull requests and selected lowest stack members produce
// single-change plans so standalone merges also use GitHub's asynchronous API.
func (r *Repository) PlanMergeRanges(
	ctx context.Context,
	changes []forge.StackChange,
) ([]forge.MergeRangePlan, error) {
	type requestedChange struct {
		change forge.ChangeID
		base   int
	}

	requestedByNumber := make(map[int]requestedChange, len(changes))
	numbers := make([]int, len(changes))
	for i, change := range changes {
		number := mustPR(change.Change).Number
		if _, exists := requestedByNumber[number]; exists {
			return nil, fmt.Errorf("duplicate GitHub pull request #%d", number)
		}
		numbers[i] = number
		requestedByNumber[number] = requestedChange{change: change.Change}
	}
	for _, change := range changes {
		if change.BaseChange == nil {
			continue
		}
		number := mustPR(change.Change).Number
		base := mustPR(change.BaseChange).Number
		if _, included := requestedByNumber[base]; included {
			requested := requestedByNumber[number]
			requested.base = base
			requestedByNumber[number] = requested
		}
	}

	pullRequests, err := r.gateway.PullRequestsForMergeRange(
		ctx,
		r.owner,
		r.repo,
		numbers,
	)
	if err != nil {
		return nil, fmt.Errorf("inspect GitHub pull requests for merge planning: %w", err)
	}
	if len(pullRequests) != len(numbers) {
		return nil, fmt.Errorf(
			"inspect GitHub pull requests for merge planning: got %d results for %d changes",
			len(pullRequests),
			len(numbers),
		)
	}

	var plans []*githubMergeRangePlan
	stacksByNumber := make(map[int]*github.PullRequestStack)
	for i, pullRequest := range pullRequests {
		number := numbers[i]
		if pullRequest == nil {
			return nil, fmt.Errorf(
				"inspect GitHub pull request #%d: %w", number, forge.ErrNotFound,
			)
		}
		if pullRequest.Stack == nil {
			plans = append(plans, &githubMergeRangePlan{
				repository: r,
				changes:    []forge.ChangeID{requestedByNumber[number].change},
			})
			continue
		}
		stacksByNumber[pullRequest.Stack.Number] = pullRequest.Stack
	}

	// GitHub merges only a lowest open prefix of a native stack. Follow each
	// remote stack from its bottom while the selected forest contains the same
	// immediate relationships. A missing lower member or a local divergence
	// stops the range; remaining selected changes stay available to the
	// handler's ordinary merge path.
	for _, stack := range stacksByNumber {
		var planned []forge.ChangeID
		previous := 0
		for _, member := range stack.Members {
			if member.State != github.PullRequestStateOpen {
				continue
			}
			number := member.Number
			requested, selected := requestedByNumber[number]
			if !selected || previous != 0 && requested.base != previous {
				break
			}
			planned = append(planned, requested.change)
			previous = number
		}
		if len(planned) > 0 {
			plans = append(plans, &githubMergeRangePlan{
				repository: r,
				changes:    planned,
			})
		}
	}

	slices.SortFunc(plans, func(left, right *githubMergeRangePlan) int {
		return mustPR(left.changes[0]).Number - mustPR(right.changes[0]).Number
	})
	result := make([]forge.MergeRangePlan, len(plans))
	for i, plan := range plans {
		result[i] = plan
	}
	return result, nil
}

// githubMergeRangePlan binds GitHub's observed stack membership to the
// operation that can safely consume it.
// Merge reloads provider state because queue preparation may restack and
// republish branches after planning.
type githubMergeRangePlan struct {
	repository *Repository
	changes    []forge.ChangeID
}

func (p *githubMergeRangePlan) Changes() []forge.ChangeID {
	return p.changes
}

func (p *githubMergeRangePlan) Merge(
	ctx context.Context,
	request forge.MergeRangeRequest,
) (forge.MergeOperation, error) {
	if len(request.Changes) != len(p.changes) {
		return nil, fmt.Errorf(
			"merge range request has %d changes, planned %d",
			len(request.Changes),
			len(p.changes),
		)
	}
	for i, change := range request.Changes {
		if change.Change.String() != p.changes[i].String() {
			return nil, fmt.Errorf(
				"merge range request change %d is %v, planned %v",
				i,
				change.Change,
				p.changes[i],
			)
		}
	}

	mergeRange, err := newGitHubMergeRange(p.repository, request)
	if err != nil {
		return nil, err
	}
	if err := mergeRange.loadPullRequests(ctx); err != nil {
		return nil, err
	}
	if err := mergeRange.validatePullRequests(); err != nil {
		return nil, err
	}
	return mergeRange.start(ctx)
}

// githubMergeRange owns one conversion from a requested forge path to a GitHub
// stack-prefix merge request.
//
// Construction establishes the bottom-to-top linear path. loadPullRequests
// then attaches GitHub's two-read remote view to the corresponding change
// records, and validatePullRequests checks that view against the requested path
// and native-stack constraints. start is the only phase that may create a
// remote merge operation; GitHub rechecks the requested top head there.
type githubMergeRange struct {
	repository *Repository
	method     forge.MergeMethod
	changes    []githubMergeRangeChange
}

// githubMergeRangeChange keeps one expected relationship, repository-local
// pull request number, and attached remote view together across all phases.
type githubMergeRangeChange struct {
	expected    forge.MergeRangeChange
	number      int
	pullRequest *github.MergeRangePullRequest
}

func newGitHubMergeRange(
	repository *Repository,
	request forge.MergeRangeRequest,
) (*githubMergeRange, error) {
	if len(request.Changes) == 0 {
		return nil, errors.New("merge range must not be empty")
	}

	changes := make([]githubMergeRangeChange, len(request.Changes))
	for i, change := range request.Changes {
		if i > 0 && request.Changes[i-1].Head != change.Base {
			return nil, fmt.Errorf(
				"merge range change #%d base branch %q does not match previous head branch %q",
				mustPR(change.Change).Number,
				change.Base,
				request.Changes[i-1].Head,
			)
		}
		if change.Base == "" {
			return nil, fmt.Errorf("merge range change %d has no base branch", i)
		}
		if change.Head == "" {
			return nil, fmt.Errorf("merge range change %d has no head branch", i)
		}
		if change.HeadHash.IsZero() {
			return nil, fmt.Errorf("merge range change %d has no head commit", i)
		}
		changes[i] = githubMergeRangeChange{
			expected: change,
			number:   mustPR(change.Change).Number,
		}
	}

	return &githubMergeRange{
		repository: repository,
		method:     request.Method,
		changes:    changes,
	}, nil
}

// loadPullRequests attaches GitHub's compact merge-range view to each requested
// change. The gateway preserves input order, so the positional relationship is
// consumed once here rather than propagated through later phases.
func (m *githubMergeRange) loadPullRequests(ctx context.Context) error {
	numbers := make([]int, len(m.changes))
	for i, change := range m.changes {
		numbers[i] = change.number
	}
	pullRequests, err := m.repository.gateway.PullRequestsForMergeRange(
		ctx,
		m.repository.owner,
		m.repository.repo,
		numbers,
	)
	if err != nil {
		return fmt.Errorf("inspect GitHub pull requests for merge range: %w", err)
	}
	for i, pullRequest := range pullRequests {
		m.changes[i].pullRequest = pullRequest
	}
	return nil
}

// validatePullRequests checks the remote view against the requested open path.
// This is the last phase before a remote merge may start, so any error from
// this method guarantees that no merge was submitted.
func (m *githubMergeRange) validatePullRequests() error {
	for i := range m.changes {
		if err := m.validatePullRequest(&m.changes[i]); err != nil {
			return err
		}
	}
	return m.validateNativeStack()
}

func (m *githubMergeRange) validatePullRequest(
	change *githubMergeRangeChange,
) error {
	number := change.number
	pullRequest := change.pullRequest
	expected := change.expected
	if pullRequest == nil {
		return fmt.Errorf(
			"inspect GitHub pull request #%d: %w", number, forge.ErrNotFound,
		)
	}
	if pullRequest.State != github.PullRequestStateOpen {
		return fmt.Errorf("GitHub pull request #%d is not open", number)
	}
	if pullRequest.IsDraft {
		return fmt.Errorf("GitHub pull request #%d is a draft", number)
	}
	if pullRequest.BaseRefName != expected.Base {
		return fmt.Errorf(
			"GitHub pull request #%d base branch is %q, expected %q",
			number,
			pullRequest.BaseRefName,
			expected.Base,
		)
	}
	if pullRequest.HeadRefName != expected.Head {
		return fmt.Errorf(
			"GitHub pull request #%d head branch is %q, expected %q",
			number,
			pullRequest.HeadRefName,
			expected.Head,
		)
	}
	if pullRequest.HeadRefOID != expected.HeadHash.String() {
		return fmt.Errorf(
			"GitHub pull request #%d head commit is %q, expected %q",
			number,
			pullRequest.HeadRefOID,
			expected.HeadHash,
		)
	}
	if len(m.changes) > 1 &&
		(!strings.EqualFold(pullRequest.HeadRepositoryOwner, m.repository.owner) ||
			!strings.EqualFold(pullRequest.HeadRepositoryName, m.repository.repo)) {
		return fmt.Errorf(
			"GitHub pull request #%d must use a head branch in %s for a stack merge",
			number,
			m.repository.owner+"/"+m.repository.repo,
		)
	}
	return nil
}

// validateNativeStack checks that the observed multi-change view is the lowest
// open prefix of one native stack. GitHub's asynchronous merge of the top pull
// request would otherwise merge a different set than the caller requested.
func (m *githubMergeRange) validateNativeStack() error {
	var stack *github.PullRequestStack
	for _, change := range m.changes {
		pullRequest := change.pullRequest
		if pullRequest.Stack == nil {
			if len(m.changes) > 1 {
				return fmt.Errorf(
					"merge range pull request #%d is not in a GitHub native stack",
					change.number,
				)
			}
			continue
		}
		if stack != nil && stack.Number != pullRequest.Stack.Number {
			return errors.New("merge range belongs to different GitHub native stacks")
		}
		if stack == nil {
			stack = pullRequest.Stack
		}
	}
	if stack == nil {
		return nil
	}
	numbers := make([]int, len(m.changes))
	for i, change := range m.changes {
		numbers[i] = change.number
	}
	openPullRequests := openStackMemberNumbers(stack.Members)
	if len(openPullRequests) < len(numbers) || !slices.Equal(
		openPullRequests[:len(numbers)],
		numbers,
	) {
		return fmt.Errorf(
			"merge range %s must begin with the lowest open member of GitHub native stack #%d (%s)",
			formatPullRequestNumbers(numbers),
			stack.Number,
			formatPullRequestNumbers(openPullRequests),
		)
	}
	return nil
}

func (m *githubMergeRange) start(ctx context.Context) (forge.MergeOperation, error) {
	method, err := githubMergeMethod(m.method)
	if err != nil {
		return nil, err
	}
	top := m.changes[len(m.changes)-1]
	result, err := m.repository.gateway.MergePullRequestAsync(ctx, &github.MergePullRequestAsyncInput{
		Owner:             m.repository.owner,
		Repo:              m.repository.repo,
		PullRequestNumber: top.number,
		ExpectedHeadSHA:   top.expected.HeadHash.String(),
		Method:            method,
	})
	if errors.Is(err, github.ErrNotFound) {
		return nil, fmt.Errorf(
			"submit GitHub asynchronous merge: %w",
			errors.Join(forge.ErrUnsupported, err),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("submit GitHub asynchronous merge: %w", err)
	}
	status, err := githubMergeOperationStatus(result)
	if err != nil {
		return nil, err
	}
	if status == forge.MergeOperationPending {
		return &githubMergeOperation{
			repository:  m.repository,
			pullRequest: top.number,
			operationID: result.OperationID,
		}, nil
	}
	return nil, nil
}

func githubMergeMethod(method forge.MergeMethod) (github.MergeMethod, error) {
	switch method {
	case forge.MergeMethodDefault:
		return github.MergeMethodUnknown, nil
	case forge.MergeMethodMerge:
		return github.MergeMethodMerge, nil
	case forge.MergeMethodSquash:
		return github.MergeMethodSquash, nil
	case forge.MergeMethodRebase:
		return github.MergeMethodRebase, nil
	default:
		return github.MergeMethodUnknown, fmt.Errorf(
			"unsupported GitHub merge method %v", method,
		)
	}
}

type githubMergeOperation struct {
	repository  *Repository
	pullRequest int
	operationID string
}

var _ forge.MergeOperation = (*githubMergeOperation)(nil)

// Status performs one GitHub asynchronous merge status probe.
func (o *githubMergeOperation) Status(
	ctx context.Context,
) (forge.MergeOperationStatus, error) {
	result, err := o.repository.gateway.AsyncMergeResult(
		ctx,
		o.repository.owner,
		o.repository.repo,
		o.pullRequest,
		o.operationID,
	)
	if err != nil {
		return 0, fmt.Errorf("poll GitHub asynchronous merge: %w", err)
	}
	return githubMergeOperationStatus(result)
}

// githubMergeOperationStatus is the forge-facing interpretation shared by
// initial submission and later status probes. Both a completed merge and a
// merge-queue admission release the caller to observe pull request state;
// neither requires another asynchronous-operation probe.
func githubMergeOperationStatus(
	result *github.AsyncMergeResult,
) (forge.MergeOperationStatus, error) {
	if result == nil {
		return 0, errors.New("GitHub asynchronous merge returned no result")
	}
	switch result.Status {
	case github.AsyncMergeStatusPending:
		return forge.MergeOperationPending, nil
	case github.AsyncMergeStatusMerged, github.AsyncMergeStatusEnqueued:
		return forge.MergeOperationAccepted, nil
	case github.AsyncMergeStatusFailed:
		return 0, asyncMergeFailure(result.Message)
	default:
		return 0, fmt.Errorf(
			"GitHub asynchronous merge returned unknown status %v", result.Status,
		)
	}
}

func asyncMergeFailure(message string) error {
	if strings.TrimSpace(message) == "" {
		message = "GitHub did not provide a failure reason"
	}
	return fmt.Errorf("GitHub asynchronous merge failed: %s", message)
}

func formatPullRequestNumbers(numbers []int) string {
	formatted := make([]string, len(numbers))
	for i, number := range numbers {
		formatted[i] = fmt.Sprintf("#%d", number)
	}
	return strings.Join(formatted, ", ")
}
