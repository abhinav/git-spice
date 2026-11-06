package github

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
	"pgregory.net/rapid"
)

func TestReconcileGitHubStacks_validTransitions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := stackResourceModelGenerator().Draw(t, "repository")
		// Reuse the mutated repository for successive desired forests.
		// Later updates must work with resources created, extended, or partly
		// dissolved by earlier updates, not just with the generator's setup.
		steps := rapid.IntRange(1, 5).Draw(t, "updates")
		for step := range steps {
			// Closing a PR preserves its base and native membership. Include
			// closures between updates so later plans encounter closed members
			// in resources established by an earlier reconciliation.
			for _, number := range slices.Sorted(maps.Keys(model.pullRequests)) {
				if model.pullRequests[number].state == github.PullRequestStateOpen &&
					rapid.IntRange(0, 7).Draw(t, fmt.Sprintf("close[%d]", number)) == 1 {
					model.closePullRequest(number)
				}
			}
			t.Logf("update %d: remote refs %v", step, slices.Sorted(maps.Keys(model.branches)))
			before := maps.Clone(model.stacks)
			beforeBases := make(map[int]string, len(model.pullRequests))
			var numbers []int
			for number := range model.pullRequests {
				numbers = append(numbers, number)
			}
			slices.Sort(numbers)
			for _, number := range numbers {
				pr := model.pullRequests[number]
				beforeBases[number] = pr.base
				t.Logf("PR #%d: %+v", number, *pr)
			}
			t.Logf("existing stacks: %v", before)

			order := rapid.
				SliceOfNDistinct(rapid.SampledFrom(numbers), 0, len(numbers), rapid.ID[int]).
				Draw(t, "desiredOrder")
			desired := stackForestGenerator(order).Draw(t, "desiredBases")
			// Zero in desired means no selected base PR, not "targets main".
			// Its target can be any published branch outside the selected PRs.
			// Excluding selected heads here keeps every relationship between
			// selected PRs represented by an edge in desired, rather than hiding
			// one in a root's branch name. Deleted heads cannot be new targets.
			externalBases := make(map[string]struct{})
			maps.Copy(externalBases, model.branches)
			for _, number := range order {
				delete(externalBases, model.pullRequests[number].head)
			}
			rootBases := slices.Sorted(maps.Keys(externalBases))
			requestedBases := make(map[int]string, len(order))
			var changes []githubStackChange
			for _, number := range order {
				base := desired[number]
				if base != 0 {
					if _, exists := model.branches[model.pullRequests[base].head]; !exists {
						base = 0
						desired[number] = 0
					}
				}
				baseBranch := rapid.SampledFrom(rootBases).Draw(t, "rootBase")
				if base != 0 {
					baseBranch = model.pullRequests[base].head
				}
				requestedBases[number] = baseBranch
				changes = append(changes, githubStackChange{
					number:           number,
					baseNumber:       base,
					baseBranch:       baseBranch,
					pullRequest:      model.snapshot(number),
					headInRepository: model.pullRequests[number].headInRepository,
				})
			}
			t.Logf("desired bases: %v", desired)
			model.trace = nil // setup operations are not reconciliation writes

			// The model checks each write at the point it is attempted.
			// A valid final forest alone would miss an illegal intermediate write,
			// such as retargeting a PR before releasing its native membership.
			got := reconcileGitHubStacks(changes)
			require.Empty(t, got.errs)
			err := executeGitHubStackProgram(t.Context(), &Repository{
				owner: "acme", repo: "repo", gateway: model,
			}, got.program)
			t.Logf("mutations: %v", model.trace)
			require.NoError(t, err)

			// Check only relationships established by this update.
			// Existing stacks can remain incompatible with the requested forest
			// when their trees are omitted.
			// This does not prescribe a selected path or require any mutation.
			for stack, members := range model.stacks {
				if slices.Equal(before[stack], members) {
					continue
				}
				var below int
				for _, number := range members {
					// A merged prefix may remain as history, but a closed member
					// still blocks the open suffix and is not desired membership.
					require.NotEqual(t, github.PullRequestStateClosed, model.pullRequests[number].state,
						"updated stack #%d retains closed PR #%d", stack, number)
					if model.pullRequests[number].state != github.PullRequestStateOpen {
						continue
					}
					base, included := desired[number]
					require.True(t, included, "stack #%d includes unrequested PR #%d", stack, number)
					if below != 0 {
						// Follow the requested bases directly rather than sharing the
						// planner's projection or path-selection machinery.
						for base != 0 && model.pullRequests[base].state != github.PullRequestStateOpen {
							base = desired[base]
						}
						assert.Equal(t, below, base, "stack #%d: PR #%d has an unintended base", stack, number)
					}
					below = number
				}
			}
			for number, pr := range model.pullRequests {
				// Replacing an active suffix must not rewrite its merged history,
				// even when those historical PRs also appear in the desired forest.
				if pr.state == github.PullRequestStateMerged {
					assert.Equal(t, beforeBases[number], pr.base, "changed merged PR #%d base", number)
					continue
				}
				if pr.base == beforeBases[number] {
					continue
				}
				_, included := desired[number]
				require.True(t, included, "changed base of unrequested PR #%d", number)
				assert.Equal(t, requestedBases[number], pr.base, "PR #%d", number)
			}
		}
	})
}

func TestStackResourceModel_baseRestrictions(t *testing.T) {
	model := &stackResourceModel{
		pullRequests: map[int]*stackResourceModelPR{
			1: {head: "a", base: "main", state: github.PullRequestStateOpen, headInRepository: true},
			2: {head: "b", base: "a", state: github.PullRequestStateOpen, headInRepository: true},
		},
		stacks:   map[int][]int{42: {1, 2}},
		branches: map[string]struct{}{"main": {}, "a": {}, "b": {}, "without-pr": {}},
	}
	ctx := t.Context()
	assert.Error(t, model.UpdatePullRequest(ctx, &github.UpdatePullRequestInput{
		PullRequestID: "2", BaseRefName: new("a"),
	}), "even a no-op base write requires unstacking")
	_, err := model.UnstackPullRequestStack(ctx, &github.UnstackPullRequestStackInput{
		Owner: "acme", Repo: "repo", StackNumber: 42,
	})
	require.NoError(t, err)
	assert.Error(t, model.UpdatePullRequest(ctx, &github.UpdatePullRequestInput{
		PullRequestID: "2", BaseRefName: new("not-pushed"),
	}))
	assert.Equal(t, "a", model.pullRequests[2].base, "rejected writes leave the base unchanged")
	require.NoError(t, model.UpdatePullRequest(ctx, &github.UpdatePullRequestInput{
		PullRequestID: "2", BaseRefName: new("without-pr"),
	}))
	assert.Equal(t, "without-pr", model.pullRequests[2].base, "a remote base does not need a PR")
}

func TestStackResourceModel_restructure(t *testing.T) {
	model := &stackResourceModel{
		pullRequests: map[int]*stackResourceModelPR{
			1: {head: "a", base: "main", state: github.PullRequestStateOpen, headInRepository: true},
			2: {head: "b", base: "a", state: github.PullRequestStateOpen, headInRepository: true},
			3: {head: "c", base: "a", state: github.PullRequestStateOpen, headInRepository: true},
		},
		stacks:   make(map[int][]int),
		branches: map[string]struct{}{"main": {}, "a": {}, "b": {}, "c": {}},
	}
	ctx := t.Context()
	require.NoError(t, model.CreatePullRequestStack(ctx, &github.CreatePullRequestStackInput{
		Owner: "acme", Repo: "repo",
		PullRequests: []int{1, 2},
	}))
	retarget := &github.UpdatePullRequestInput{PullRequestID: "2", BaseRefName: new("c")}
	assert.Error(t, model.UpdatePullRequest(ctx, retarget), "stacked bases cannot change")
	assert.Error(t, model.AddPullRequestsToStack(ctx, &github.AddPullRequestsToStackInput{
		Owner: "acme", Repo: "repo",
		StackNumber: 1, PullRequests: []int{3},
	}), "append must continue from the top, not insert in the middle")
	assert.Error(t, model.CreatePullRequestStack(ctx, &github.CreatePullRequestStackInput{
		Owner: "acme", Repo: "repo",
		PullRequests: []int{1, 3, 2},
	}), "existing membership must be dissolved first")

	result, err := model.UnstackPullRequestStack(ctx, &github.UnstackPullRequestStackInput{
		Owner: "acme", Repo: "repo", StackNumber: 1,
	})
	require.NoError(t, err)
	require.Empty(t, result.RemainingPullRequests)
	assert.Equal(t, "a", model.pullRequests[2].base, "unstack does not retarget PRs")
	require.NoError(t, model.UpdatePullRequest(ctx, retarget))
	require.NoError(t, model.CreatePullRequestStack(ctx, &github.CreatePullRequestStackInput{
		Owner: "acme", Repo: "repo",
		PullRequests: []int{1, 3, 2},
	}))
}

func TestStackResourceModel_restructureAfterMergedPrefix(t *testing.T) {
	model := &stackResourceModel{
		pullRequests: map[int]*stackResourceModelPR{
			1: {head: "a", base: "main", state: github.PullRequestStateMerged, headInRepository: true},
			2: {head: "b", base: "main", state: github.PullRequestStateOpen, headInRepository: true},
			3: {head: "c", base: "b", state: github.PullRequestStateOpen, headInRepository: true},
			4: {head: "d", base: "c", state: github.PullRequestStateOpen, headInRepository: true},
		},
		stacks:    map[int][]int{42: {1, 2, 3, 4}},
		branches:  map[string]struct{}{"main": {}, "b": {}, "c": {}, "d": {}},
		nextStack: 42,
	}
	result, err := model.UnstackPullRequestStack(t.Context(), &github.UnstackPullRequestStackInput{
		Owner: "acme", Repo: "repo", StackNumber: 42,
	})
	require.NoError(t, err)
	assert.Equal(t, []int{1}, result.RemainingPullRequests)
	assert.Equal(t, []int{1}, model.stacks[42])

	// Historical membership on #1 does not prevent recreating the released
	// active suffix without #3. Removing #3 leaves its PR and base unchanged.
	require.NoError(t, model.UpdatePullRequest(t.Context(), &github.UpdatePullRequestInput{
		PullRequestID: "4", BaseRefName: new("b"),
	}))
	require.NoError(t, model.CreatePullRequestStack(t.Context(), &github.CreatePullRequestStackInput{
		Owner: "acme", Repo: "repo", PullRequests: []int{2, 4},
	}))
	assert.Equal(t, map[int][]int{42: {1}, 43: {2, 4}}, model.stacks)
	assert.Equal(t, "b", model.pullRequests[3].base)
}

func TestStackResourceModel_closedMember(t *testing.T) {
	model := &stackResourceModel{
		pullRequests: map[int]*stackResourceModelPR{
			1: {head: "a", base: "main", state: github.PullRequestStateOpen, headInRepository: true},
			2: {head: "b", base: "a", state: github.PullRequestStateOpen, headInRepository: true},
			3: {head: "c", base: "a", state: github.PullRequestStateOpen, headInRepository: true},
		},
		stacks:    map[int][]int{42: {1, 2}},
		branches:  map[string]struct{}{"main": {}, "a": {}, "b": {}, "c": {}},
		nextStack: 42,
	}
	model.closePullRequest(2)
	assert.Equal(t, github.PullRequestStateClosed, model.snapshot(2).State)
	assert.Equal(t, []github.PullRequestStackMember{
		{Number: 1, State: github.PullRequestStateOpen},
		{Number: 2, State: github.PullRequestStateClosed},
	}, model.snapshot(1).Stack.Members)
	assert.Equal(t, "a", model.pullRequests[2].base)
	assert.Contains(t, model.branches, "b")

	// Closing the top does not make the last open member the append target.
	assert.Error(t, model.AddPullRequestsToStack(t.Context(), &github.AddPullRequestsToStackInput{
		Owner: "acme", Repo: "repo", StackNumber: 42, PullRequests: []int{3},
	}))
	assert.Equal(t, []int{1, 2}, model.stacks[42])
	result, err := model.UnstackPullRequestStack(t.Context(), &github.UnstackPullRequestStackInput{
		Owner: "acme", Repo: "repo", StackNumber: 42,
	})
	require.NoError(t, err)
	assert.Empty(t, result.RemainingPullRequests, "closed members are released, unlike merged history")
	require.NoError(t, model.CreatePullRequestStack(t.Context(), &github.CreatePullRequestStackInput{
		Owner: "acme", Repo: "repo", PullRequests: []int{1, 3},
	}))
	assert.Equal(t, map[int][]int{43: {1, 3}}, model.stacks)
	assert.Equal(t, github.PullRequestStateClosed, model.pullRequests[2].state)
	assert.Equal(t, "a", model.pullRequests[2].base)
}

// stackResourceModel models GitHub's native stack operations.
// It excludes Git commits, transport, and path selection.
// It enforces GitHub's observed write restrictions
// independently of the reconciliation plan.
type stackResourceModel struct {
	githubGateway

	// pullRequests maps repository-local PR numbers to their current state.
	pullRequests map[int]*stackResourceModelPR

	// branches is the complete set of refs in the receiving repository.
	// PRs do not imply ref existence: a branch can have no PR, and a merged
	// PR can remain in native history after its head ref has been deleted.
	branches map[string]struct{}

	// stacks maps repository-local stack numbers to member PR numbers,
	// ordered bottom-to-top, including closed, merged, and locked members.
	stacks map[int][]int

	// nextStack is the last assigned stack number.
	// Dissolving a stack does not make its number available for reuse.
	nextStack int

	// trace records attempted mutations, including writes the model rejects.
	// The property clears it before each update and logs that update's writes.
	trace []string
}

// stackResourceModelPR holds the current state of one PR.
// Native membership is recorded separately in stackResourceModel.stacks.
type stackResourceModelPR struct {
	// head is the source branch name used to check the next stack member's base.
	head string

	// base is the current target branch name, not the base PR number.
	base string

	// state controls membership eligibility and retention.
	// Only open PRs can join a stack. Closing preserves existing membership;
	// unstacking releases closed PRs but retains merged history.
	state github.PullRequestState

	// headInRepository is false for fork PRs, which cannot join native stacks.
	headInRepository bool

	// locked means queued for merge or auto-merge enabled.
	// These members survive unstacking even while they are open.
	locked bool
}

var _ githubGateway = (*stackResourceModel)(nil)

func (m *stackResourceModel) closePullRequest(number int) {
	m.trace = append(m.trace, fmt.Sprintf("close #%d", number))
	pr := m.pullRequests[number]
	pr.state = github.PullRequestStateClosed
	pr.locked = false
}

func (m *stackResourceModel) CreatePullRequestStack(
	_ context.Context, req *github.CreatePullRequestStackInput,
) error {
	m.trace = append(m.trace, fmt.Sprintf("create %v", req.PullRequests))
	if len(req.PullRequests) < 2 || len(req.PullRequests) > 100 {
		return errors.New("create requires 2-100 PRs")
	}
	if err := m.checkNewMembers(req.PullRequests, 0); err != nil {
		return err
	}
	m.nextStack++
	m.stacks[m.nextStack] = slices.Clone(req.PullRequests)
	return nil
}

func (m *stackResourceModel) AddPullRequestsToStack(
	_ context.Context, req *github.AddPullRequestsToStackInput,
) error {
	m.trace = append(m.trace, fmt.Sprintf("append %v to #%d", req.PullRequests, req.StackNumber))
	members, found := m.stacks[req.StackNumber]
	if !found || len(req.PullRequests) < 1 || len(req.PullRequests) > 100 {
		return errors.New("append requires an existing stack and 1-100 PRs")
	}
	if err := m.checkNewMembers(req.PullRequests, members[len(members)-1]); err != nil {
		return err
	}
	m.stacks[req.StackNumber] = append(slices.Clone(members), req.PullRequests...)
	return nil
}

func (m *stackResourceModel) checkNewMembers(numbers []int, below int) error {
	seen := make(map[int]struct{})
	for _, number := range numbers {
		pr := m.pullRequests[number]
		if pr == nil || pr.state != github.PullRequestStateOpen || !pr.headInRepository {
			return fmt.Errorf("PR #%d is not eligible for native membership", number)
		}
		if _, exists := m.branches[pr.head]; !exists {
			return fmt.Errorf("PR #%d head branch %q does not exist", number, pr.head)
		}
		if _, exists := m.branches[pr.base]; !exists {
			return fmt.Errorf("PR #%d base branch %q does not exist", number, pr.base)
		}
		if _, ok := seen[number]; ok || m.stackFor(number) != 0 {
			return fmt.Errorf("PR #%d already has membership", number)
		}
		if below != 0 && pr.base != m.pullRequests[below].head {
			return fmt.Errorf("PR #%d base %q does not continue from PR #%d", number, pr.base, below)
		}
		seen[number] = struct{}{}
		below = number
	}
	return nil
}

func (m *stackResourceModel) UnstackPullRequestStack(
	_ context.Context, req *github.UnstackPullRequestStackInput,
) (*github.UnstackPullRequestStackResult, error) {
	m.trace = append(m.trace, fmt.Sprintf("unstack #%d", req.StackNumber))
	members, found := m.stacks[req.StackNumber]
	if !found {
		return nil, fmt.Errorf("stack #%d does not exist", req.StackNumber)
	}
	var remaining []int
	for _, number := range members {
		pr := m.pullRequests[number]
		if pr.state == github.PullRequestStateMerged || pr.locked {
			remaining = append(remaining, number)
		}
	}
	delete(m.stacks, req.StackNumber)
	if len(remaining) > 0 {
		m.stacks[req.StackNumber] = remaining
	}
	return &github.UnstackPullRequestStackResult{RemainingPullRequests: remaining}, nil
}

func (m *stackResourceModel) UpdatePullRequest(
	_ context.Context, req *github.UpdatePullRequestInput,
) error {
	number, err := strconv.Atoi(string(req.PullRequestID))
	if err != nil || m.pullRequests[number] == nil || req.BaseRefName == nil {
		return errors.New("retarget requires a known PR and base")
	}
	m.trace = append(m.trace, fmt.Sprintf("retarget %s to %q", req.PullRequestID, *req.BaseRefName))
	if m.stackFor(number) != 0 {
		return fmt.Errorf("PR #%d must be unstacked before changing its base", number)
	}
	if _, exists := m.branches[*req.BaseRefName]; !exists {
		return fmt.Errorf("base branch %q does not exist", *req.BaseRefName)
	}
	m.pullRequests[number].base = *req.BaseRefName
	return nil
}

func (*stackResourceModel) CheckPullRequestStacks(context.Context, string, string) error {
	return nil
}

func (m *stackResourceModel) PullRequestsForStackUpdate(
	_ context.Context, _, _ string, numbers []int,
) ([]*github.StackUpdatePullRequest, error) {
	results := make([]*github.StackUpdatePullRequest, len(numbers))
	for i, number := range numbers {
		results[i] = m.snapshot(number)
		if m.pullRequests[number].headInRepository {
			results[i].HeadRepositoryOwner = "acme"
			results[i].HeadRepositoryName = "repo"
		}
	}
	return results, nil
}

func (m *stackResourceModel) stackFor(number int) int {
	for stack, members := range m.stacks {
		if slices.Contains(members, number) {
			return stack
		}
	}
	return 0
}

// snapshot copies the current provider state
// so executing the model cannot change the inputs used to compile the program.
func (m *stackResourceModel) snapshot(number int) *github.StackUpdatePullRequest {
	pr := m.pullRequests[number]
	result := &github.StackUpdatePullRequest{
		ID:          github.ID(strconv.Itoa(number)),
		State:       pr.state,
		HeadRefName: pr.head,
		BaseRefName: pr.base,
	}
	if stack := m.stackFor(number); stack != 0 {
		result.Stack = &github.PullRequestStack{Number: stack}
		for _, member := range m.stacks[stack] {
			result.Stack.Members = append(result.Stack.Members, github.PullRequestStackMember{
				Number: member,
				State:  m.pullRequests[member].state,
				Locked: m.pullRequests[member].locked,
			})
		}
	}
	return result
}

func stackResourceModelGenerator() *rapid.Generator[*stackResourceModel] {
	branchNameGenerator := rapid.StringMatching(`[a-z]{1,8}`).
		Filter(func(name string) bool { return name != "main" })
	prNumGenerator := rapid.IntRange(1, 1000)

	return rapid.Custom(func(t *rapid.T) *stackResourceModel {
		// Generate the repository's refs independently from its PR forest.
		// The first prCount names become PR heads; the rest remain valid base
		// targets without PRs. Later, merged PRs may outlive their head refs.
		prCount := rapid.IntRange(1, 100).Draw(t, "prCount")
		branchOnlyCount := rapid.IntRange(0, 20).Draw(t, "branchesWithoutPRs")
		branchCount := prCount + branchOnlyCount
		names := rapid.SliceOfNDistinct(branchNameGenerator, branchCount, branchCount, rapid.ID[string]).Draw(t, "branches")
		prNums := rapid.
			SliceOfNDistinct(prNumGenerator, prCount, prCount, rapid.ID[int]).
			Draw(t, "prNumbers")
		baseByPR := stackForestGenerator(prNums).Draw(t, "currentBases")

		model := &stackResourceModel{
			pullRequests: make(map[int]*stackResourceModelPR, prCount),
			stacks:       make(map[int][]int),
			branches:     map[string]struct{}{"main": {}},
		}
		for _, name := range names {
			model.branches[name] = struct{}{}
		}
		rootBases := append([]string{"main"}, names[prCount:]...)
		for i, number := range prNums {
			name := names[i]

			baseBranch := rapid.SampledFrom(rootBases).Draw(t, "currentRootBase")
			if base := baseByPR[number]; base != 0 {
				baseBranch = model.pullRequests[base].head
			}

			model.pullRequests[number] = &stackResourceModelPR{
				head:             name,
				base:             baseBranch,
				state:            github.PullRequestStateOpen,
				headInRepository: true,
			}
		}

		for _, number := range prNums {
			base := baseByPR[number]
			native := rapid.Bool().Draw(t, fmt.Sprintf("native[%d]", number))
			if base == 0 || !native {
				continue
			}

			stack := model.stackFor(base)
			if stack == 0 {
				require.NoError(t, model.CreatePullRequestStack(t.Context(), &github.CreatePullRequestStackInput{
					Owner: "acme", Repo: "repo",
					PullRequests: []int{base, number},
				}))
			} else if members := model.stacks[stack]; members[len(members)-1] == base {
				require.NoError(t, model.AddPullRequestsToStack(t.Context(), &github.AddPullRequestsToStackInput{
					Owner: "acme", Repo: "repo",
					StackNumber: stack, PullRequests: []int{number},
				}))
			}
		}

		// A native stack may have a merged prefix,
		// but not an independently merged middle member.
		// When that prefix lands, the first open member targets the old bottom's
		// base rather than the last merged head. Native membership still records
		// the merged history even if those head refs are later deleted:
		//
		//   Membership: [a(merged), b(merged), c(open), d(open)]
		//   Open bases: main ─→ c ─→ d
		//
		// Leave at least one unmerged member in each stack.
		for _, stack := range slices.Sorted(maps.Keys(model.stacks)) {
			members := model.stacks[stack]
			merged := rapid.IntRange(0, len(members)-1).Draw(t, "mergedPrefix")
			base := model.pullRequests[members[0]].base
			for _, number := range members[:merged] {
				model.pullRequests[number].state = github.PullRequestStateMerged
			}
			if merged > 0 {
				model.pullRequests[members[merged]].base = base
			}
		}

		for _, number := range prNums {
			pr := model.pullRequests[number]
			if pr.state != github.PullRequestStateOpen {
				continue
			}

			// Any open PR can close, including native members. Merging an
			// individual native member is restricted to the prefix above;
			// fork PRs cannot acquire native membership in the first place.
			state := rapid.IntRange(0, 7).Draw(t, fmt.Sprintf("state[%d]", number))
			if state == 1 {
				model.closePullRequest(number)
			} else if model.stackFor(number) == 0 {
				switch state {
				case 2:
					pr.state = github.PullRequestStateMerged
				case 3:
					pr.headInRepository = false
				}
			}

			// Include some queued or auto-merge PRs to exercise unstack retention.
			locked := rapid.IntRange(0, 7).Draw(t, fmt.Sprintf("locked[%d]", number))
			pr.locked = pr.state == github.PullRequestStateOpen && locked == 1
		}

		// PR history outlives its ref. Keep refs that open PRs still target,
		// and independently remove some unused merged heads. Unstacking must
		// release active members without requiring that historical ref to exist.
		for _, number := range prNums {
			pr := model.pullRequests[number]
			if pr.state != github.PullRequestStateMerged {
				continue
			}
			used := false
			for _, above := range model.pullRequests {
				used = used || (above.state == github.PullRequestStateOpen && above.base == pr.head)
			}
			if !used && rapid.Bool().Draw(t, "deleteMergedHead") {
				delete(model.branches, pr.head)
			}
		}
		return model
	})
}

// stackForestGenerator generates maps from PR numbers to their bases.
// Each base is either outside the forest (zero) or an earlier PR in prs.
// Selecting only earlier PRs prevents cycles.
func stackForestGenerator(prs []int) *rapid.Generator[map[int]int] {
	// Empty and single-PR forests have no base choice to draw.
	if len(prs) < 2 {
		bases := make(map[int]int, len(prs))
		for _, number := range prs {
			bases[number] = 0
		}
		return rapid.Just(bases)
	}

	return rapid.Custom(func(t *rapid.T) map[int]int {
		bases := make(map[int]int, len(prs))
		available := []int{0}
		for _, prNum := range prs {
			bases[prNum] = rapid.SampledFrom(available).Draw(t, "base")
			available = append(available, prNum)
		}
		return bases
	})
}
