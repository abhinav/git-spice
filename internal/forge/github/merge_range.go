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
// Each native range is a lowest open prefix whose members follow the supplied
// immediate-base relationships; unstacked pull requests receive single-change plans.
// Other requested changes remain available for ordinary per-change merging.
func (r *Repository) PlanMergeRanges(
	ctx context.Context,
	changes []forge.MergePlanChange,
) ([]forge.MergeRangePlan, error) {
	if !r.stacksEnabled {
		return nil, forge.ErrUnsupported
	}

	// Probe the REST capability before querying GraphQL stack fields.
	// Servers without native stacks may not expose those fields at all.
	if err := r.gateway.CheckPullRequestStacks(ctx, r.owner, r.repo); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return nil, errors.Join(forge.ErrUnsupported, err)
		}
		return nil, fmt.Errorf("check GitHub native stack support: %w", err)
	}

	type requestedPR struct {
		PR   *PR
		Base int
	}

	requestByNumber := make(map[int]requestedPR, len(changes))
	numbers := make([]int, len(changes))
	for i, change := range changes {
		pr := mustPR(change.Change)

		var base int
		if change.BaseChange != nil {
			base = mustPR(change.BaseChange).Number
		}

		numbers[i] = pr.Number
		requestByNumber[pr.Number] = requestedPR{
			PR:   pr,
			Base: base,
		}
	}

	prs, err := r.gateway.PullRequestsForMergeRange(ctx, r.owner, r.repo, numbers)
	if err != nil {
		return nil, fmt.Errorf("inspect GitHub pull requests for merge planning: %w", err)
	}

	// Collect the stacks that GitHub reports.
	// They all have the same contents,
	// but the API returns a separate copy for each member.
	var stacks []*github.PullRequestStack
	seenStacks := make(map[int]struct{})

	// Take a first pass, grouping PRs by stacks they belong to.
	// Anything that's not part of a GitHub-native stack
	// is merged individually.
	var plans []forge.MergeRangePlan

	for i, pr := range prs {
		prNum := numbers[i]
		// Successful batches retain nil slots for PRs GitHub did not find.
		if pr == nil {
			return nil, fmt.Errorf("inspect PR #%d: %w", prNum, forge.ErrNotFound)
		}
		if pr.Stack == nil {
			plans = append(plans, &githubMergeRangePlan{
				Repo: r,
				PRs:  []*PR{requestByNumber[prNum].PR},
			})
			continue
		}

		if _, ok := seenStacks[pr.Stack.Number]; !ok {
			seenStacks[pr.Stack.Number] = struct{}{}
			stacks = append(stacks, pr.Stack)
		}
	}

	// GitHub merges only a lowest open prefix of a native stack.
	// Follow each remote stack from its bottom
	// while the selected forest contains the same immediate relationships.
	//
	// A missing lower member or a local divergence stops the range;
	// remaining selected changes stay available for ordinary merge.
	for _, stack := range stacks {
		var planned []*PR

		var previous int
		for _, member := range stack.Members {
			if member.State != github.PullRequestStateOpen {
				continue
			}
			number := member.Number
			req, ok := requestByNumber[number]
			if !ok || previous != 0 && req.Base != previous {
				break
			}

			planned = append(planned, req.PR)
			previous = number
		}

		if len(planned) > 0 {
			plans = append(plans, &githubMergeRangePlan{
				Repo: r,
				PRs:  planned,
			})
		}
	}

	slices.SortFunc(plans, func(left, right forge.MergeRangePlan) int {
		return mustPR(left.Changes()[0]).Number - mustPR(right.Changes()[0]).Number
	})
	return plans, nil
}

// githubMergeRangePlan retains selected identities, not the provider snapshot.
// Queue preparation may restack and republish these branches before Merge,
// so the plan reloads their current relationships before submitting a merge.
type githubMergeRangePlan struct {
	Repo *Repository // required
	PRs  []*PR
}

func (p *githubMergeRangePlan) Changes() []forge.ChangeID {
	changes := make([]forge.ChangeID, len(p.PRs))
	for i, pr := range p.PRs {
		changes[i] = pr
	}
	return changes
}

func (p *githubMergeRangePlan) Merge(
	ctx context.Context,
	request forge.MergeRangeRequest,
) (forge.MergeOperation, error) {
	if !p.Repo.stacksEnabled {
		return nil, forge.ErrUnsupported
	}

	prNums := make([]int, len(p.PRs))
	for i, pr := range p.PRs {
		prNums[i] = pr.Number
	}
	expectedHeads := make(map[int]string, len(request.Heads))
	for _, head := range request.Heads {
		expectedHeads[mustPR(head.Change).Number] = head.HeadHash.String()
	}

	// Planning happens before queue preparation can restack and republish
	// these branches, changing PR heads, bases, or native membership.
	// Read the provider state again to compare with the prepared request.
	// The gateway preserves input order across two API reads;
	// this is an observed view, not an atomic snapshot.
	prs, err := p.Repo.gateway.PullRequestsForMergeRange(
		ctx,
		p.Repo.owner,
		p.Repo.repo,
		prNums,
	)
	if err != nil {
		return nil, fmt.Errorf("inspect GitHub pull requests for merge range: %w", err)
	}
	for i, prNum := range prNums {
		err := func(gotPR *github.MergeRangePullRequest) error {
			if gotPR == nil {
				return forge.ErrNotFound
			}
			if want, got := github.PullRequestStateOpen, gotPR.State; want != got {
				return fmt.Errorf("expected state %v, got %v", want, got)
			}
			if gotPR.IsDraft {
				return errors.New("pull request is still a draft")
			}
			if i > 0 {
				if want, got := prs[i-1].HeadRefName, gotPR.BaseRefName; want != got {
					return fmt.Errorf("expected base %q, got %q", want, got)
				}
			}
			if want, got := expectedHeads[prNum], gotPR.HeadRefOID; want != got {
				return fmt.Errorf("expected head %q, got %q", want, got)
			}

			// If there are multiple changes,
			// they must all be in the same repository (no forks).
			if len(p.PRs) > 1 {
				wantOwner, gotOwner := p.Repo.owner, gotPR.HeadRepositoryOwner
				wantRepo, gotRepo := p.Repo.repo, gotPR.HeadRepositoryName
				if !strings.EqualFold(wantOwner, gotOwner) || !strings.EqualFold(wantRepo, gotRepo) {
					return fmt.Errorf("expected repository %q, got %q", wantOwner+"/"+wantRepo, gotOwner+"/"+gotRepo)
				}
			}

			return nil
		}(prs[i])
		if err != nil {
			return nil, fmt.Errorf("merge PR #%d: %w", prNum, err)
		}

	}

	// GitHub merges the top PR together with all open members below it.
	// Matching individual heads and bases is therefore insufficient:
	// the requested numbers must be exactly the stack's lowest open prefix,
	// or the request would merge changes the caller never selected.
	// An unstacked singleton needs no membership, but a stacked singleton
	// must still be the lowest open member.
	stack := prs[0].Stack
	if stack == nil {
		if len(p.PRs) > 1 {
			return nil, fmt.Errorf(
				"merge range pull request #%d is not in a GitHub native stack",
				prNums[0],
			)
		}
	} else {
		for i := 1; i < len(prs); i++ {
			pr := prs[i]
			if pr.Stack == nil {
				return nil, fmt.Errorf(
					"merge range pull request #%d is not in a GitHub native stack",
					prNums[i],
				)
			}
			if stack.Number != pr.Stack.Number {
				return nil, errors.New("merge range belongs to different GitHub native stacks")
			}
		}
		openPRs := openStackMemberNumbers(stack.Members)
		if len(openPRs) < len(prNums) || !slices.Equal(openPRs[:len(prNums)], prNums) {
			return nil, fmt.Errorf(
				"merge range %s must begin with the lowest open member of GitHub native stack #%d (%s)",
				formatPullRequestNumbers(prNums),
				stack.Number,
				formatPullRequestNumbers(openPRs),
			)
		}
	}

	var method github.MergeMethod
	switch request.Method {
	case forge.MergeMethodMerge:
		method = github.MergeMethodMerge
	case forge.MergeMethodSquash:
		method = github.MergeMethodSquash
	case forge.MergeMethodRebase:
		method = github.MergeMethodRebase
	default:
		method = github.MergeMethodUnknown
	}
	top := p.PRs[len(p.PRs)-1]
	// The observations above are not a transaction.
	// GitHub checks this top commit again when accepting the merge request.
	return p.Repo.mergeAsync(ctx, &github.MergePullRequestAsyncInput{
		Owner:             p.Repo.owner,
		Repo:              p.Repo.repo,
		PullRequestNumber: top.Number,
		ExpectedHeadSHA:   expectedHeads[top.Number],
		Method:            method,
	})
}

func formatPullRequestNumbers(numbers []int) string {
	formatted := make([]string, len(numbers))
	for i, number := range numbers {
		formatted[i] = fmt.Sprintf("#%d", number)
	}
	return strings.Join(formatted, ", ")
}
