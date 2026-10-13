package github

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"go.abhg.dev/container/ring"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
	"go.abhg.dev/gs/internal/graph"
)

// githubStackReconciliation is the deterministic result of reconciling one
// desired forest with its observed GitHub state.
type githubStackReconciliation struct {
	transitions []githubStackTransition
	warnings    []string
	errs        []error
}

// reconcileGitHubStacks is the pure policy boundary for native-stack updates.
// It projects GitHub-ineligible pull requests, preserves compatible existing
// membership, selects GitHub's linear representation, and returns only the
// ordered mutations required to reach that representation.
func reconcileGitHubStacks(
	snapshot githubStackSnapshot,
) githubStackReconciliation {
	// Copy the immutable snapshot into a pointer-linked working forest.
	// Later phases annotate these nodes as GitHub eligibility and selected bases
	// become known without mutating caller-owned input.
	reconciler := githubStackReconciler{
		changesByNumber: make(map[int]*githubStackChange, len(snapshot)),
		orderedChanges:  make([]*githubStackChange, len(snapshot)),
	}
	for i, observed := range snapshot {
		change := &githubStackChange{
			number:      observed.desired.number,
			baseBranch:  observed.desired.baseBranch,
			pullRequest: observed.pullRequest,
		}
		reconciler.changesByNumber[change.number] = change
		reconciler.orderedChanges[i] = change
	}
	for i, observed := range snapshot {
		reconciler.orderedChanges[i].requestedBase = reconciler.changesByNumber[observed.desired.baseNumber]
	}

	// Projection removes relationships GitHub cannot represent.
	// Selection then chooses one linear path per projected tree.
	// Coalescing makes one remote stack the unit of replacement before the
	// selected paths are converted into provider writes.
	errs := reconciler.projectChangeTrees()
	candidates := reconciler.planChangeTrees()
	transitions := reconciler.coalesceReplacements(candidates)
	return githubStackReconciliation{
		transitions: transitions,
		warnings:    reconciler.warnings,
		errs:        errs,
	}
}

// githubStackReconciler owns the mutable working representation used by the
// pure reconciliation operation. Its state is allocated for one invocation
// and never escapes in the result.
type githubStackReconciler struct {
	changesByNumber map[int]*githubStackChange
	orderedChanges  []*githubStackChange

	// aboves and bottoms describe the projected forest after closed and merged
	// pull requests have been removed.
	aboves  map[*githubStackChange][]*githubStackChange
	bottoms []*githubStackChange

	warnings []string
}

// githubStackChange carries one requested relationship through GitHub lookup,
// projection, and linear-path selection.
type githubStackChange struct {
	// number is the repository-local pull request number.
	number int

	// requestedBase is the immediate base supplied in the forge request.
	requestedBase *githubStackChange

	// baseBranch is the desired provider-facing base branch.
	baseBranch string

	// pullRequest is nil when GitHub did not find the requested change.
	pullRequest *githubStackPullRequestState

	// projection records how this change participates in GitHub's
	// representation.
	projection githubStackProjection

	// nearestIncluded is this change when it is included. Transparent changes
	// inherit their requested base's value so their upstack reconnects to the
	// nearest included change below them.
	nearestIncluded *githubStackChange

	// projectedBase is the nearest included change below this change. It is set
	// only for included changes.
	projectedBase *githubStackChange
}

// githubStackProjection records how a requested change participates in GitHub's
// open, same-repository stack representation. The zero value means projection
// has not inspected the change yet.
type githubStackProjection uint8

const (
	// githubStackProjectionExcluded means GitHub cannot represent the change
	// or its requested upstack.
	githubStackProjectionExcluded githubStackProjection = iota + 1

	// githubStackProjectionTransparent means the pull request is closed or
	// merged. GitHub omits it and joins its upstack to the nearest included
	// change below.
	githubStackProjectionTransparent

	// githubStackProjectionIncluded means the pull request is open and eligible
	// for a GitHub native stack.
	githubStackProjectionIncluded
)

// projectChangeTrees converts requested forge relationships into the forest
// from which GitHub native stacks may be selected.
//
// A closed or merged pull request is transparent because its upstack remains
// valid and can reconnect to the nearest included change below it. A missing
// or cross-repository pull request instead excludes its complete requested
// upstack: GitHub cannot form a same-repository native stack through that
// boundary. Missing pull requests are genuine lookup failures as well as
// projection boundaries, so they are returned after projection completes.
func (r *githubStackReconciler) projectChangeTrees() []error {
	var errs []error
	// Project changes in base-first order. That makes each requested base's
	// exclusion and nearest included change final before its upstack is visited.
	for _, change := range r.orderedChanges {
		if change.requestedBase != nil &&
			change.requestedBase.projection == githubStackProjectionExcluded {
			change.projection = githubStackProjectionExcluded
			continue
		}

		// Closed and merged pull requests disappear from GitHub's native stack,
		// so carry the nearest included change through them. Eligible open pull
		// requests use that change as their projected base.
		var below *githubStackChange
		if change.requestedBase != nil {
			below = change.requestedBase.nearestIncluded
		}
		pullRequest := change.pullRequest
		if pullRequest == nil {
			change.projection = githubStackProjectionExcluded
			errs = append(errs, fmt.Errorf(
				"inspect GitHub pull request #%d: %w",
				change.number,
				forge.ErrNotFound,
			))
			r.warnOmittedUpstack(change.number, "the pull request was not found")
			continue
		}
		if pullRequest.state != github.PullRequestStateOpen {
			change.projection = githubStackProjectionTransparent
			change.nearestIncluded = below
			continue
		}
		if !pullRequest.headInRepository {
			change.projection = githubStackProjectionExcluded
			r.warnOmittedUpstack(
				change.number,
				"the head branch is not in the receiving repository",
			)
			continue
		}

		change.projection = githubStackProjectionIncluded
		change.nearestIncluded = change
		change.projectedBase = below
	}

	// Materialize the projected forest only after every change has been
	// classified. Excluded changes never become traversal roots or edges.
	r.aboves = make(map[*githubStackChange][]*githubStackChange, len(r.changesByNumber))
	for _, change := range r.orderedChanges {
		if change.projection != githubStackProjectionIncluded {
			continue
		}
		if change.projectedBase == nil {
			r.bottoms = append(r.bottoms, change)
			continue
		}
		r.aboves[change.projectedBase] = append(r.aboves[change.projectedBase], change)
	}

	// PR-number ordering makes independent-tree processing, equal-length path
	// selection, and divergent-upstack warnings deterministic.
	slices.SortFunc(r.bottoms, func(a, b *githubStackChange) int {
		return cmp.Compare(a.number, b.number)
	})
	for _, changes := range r.aboves {
		slices.SortFunc(changes, func(a, b *githubStackChange) int {
			return cmp.Compare(a.number, b.number)
		})
	}
	return errs
}

// planChangeTrees independently selects a desired linear path for each
// projected tree and records the transition from the observed GitHub stack.
// Selection limitations warn and omit their tree before any write is possible.
func (r *githubStackReconciler) planChangeTrees() []githubStackTransitionCandidate {
	var candidates []githubStackTransitionCandidate
	// Each bottom owns one complete projected change tree.
	// Selection inspects every reachable change
	// so existing native membership can constrain the path
	// before longest-path selection.
	// The selected path is the complete desired linear stack,
	// including any existing prefix.
	// The transition records whether execution can append the missing suffix
	// or must dissolve and recreate a changed linear composition.
	for _, bottom := range r.bottoms {
		selected, remoteStack, ok := r.selectLinearPath(bottom)
		if !ok {
			continue
		}

		// For each selected branch in the stack, if it has siblings,
		// those siblings are omitted from the native stack.
		for idx, change := range selected {
			var selectedAbove *githubStackChange
			if idx+1 < len(selected) {
				selectedAbove = selected[idx+1]
			}
			for _, above := range r.aboves[change] {
				if above != selectedAbove {
					r.warnOmittedUpstack(
						above.number,
						"the change tree diverges from the selected linear path",
					)
				}
			}
		}

		candidate := newGitHubStackTransitionCandidate(selected, remoteStack)
		if candidate.kind != githubStackTransitionCurrent {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

// selectLinearPath applies GitHub's non-divergence constraint before choosing
// among the projected tree's remaining paths.
//
// If a divergent tree intersects an existing native stack,
// that stack's complete open membership must be a compatible selected prefix.
// This preserves GitHub's one-path representation
// rather than silently replacing it with a sibling path.
// A linear desired tree may replace incompatible membership during execution.
// Multiple existing native stacks remain ambiguous and leave the tree unchanged.
// Only the upstack above a compatible prefix participates in longest-path
// selection; equal lengths prefer the lower pull request number.
//
// On success, selectLinearPath returns the selected base-up path, the remote
// stack to extend or restructure,
// or nil when GitHub must create one,
// and true.
// When divergent existing membership cannot be preserved,
// it warns, returns false,
// and the caller leaves the complete projected tree unchanged.
func (r *githubStackReconciler) selectLinearPath(
	bottom *githubStackChange,
) ([]*githubStackChange, *github.PullRequestStack, bool) {
	tree, ok := r.inspectExistingStack(bottom)
	if !ok {
		return nil, nil, false
	}

	extensionBase := bottom
	if tree.stack != nil {
		prefixTop, compatible := r.existingStackPrefix(bottom, tree.stack)
		if !compatible && tree.divergent {
			openPullRequests := openStackMemberNumbers(tree.stack.Members)
			formatted := make([]string, len(openPullRequests))
			for i, number := range openPullRequests {
				formatted[i] = fmt.Sprintf("#%d", number)
			}
			r.warnings = append(r.warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: open pull requests %s have incompatible membership",
				bottom.number,
				tree.stack.Number,
				strings.Join(formatted, ", "),
			))
			return nil, nil, false
		}
		if compatible {
			extensionBase = prefixTop
		}
	}

	// Select only above the fixed remote prefix. FurthestChildren establishes
	// maximum path length; PR number supplies GitHub's deterministic tie-break.
	tops := graph.FurthestChildren(
		extensionBase,
		func(change *githubStackChange) []*githubStackChange {
			return r.aboves[change]
		},
	)
	top := slices.MinFunc(tops, func(a, b *githubStackChange) int {
		return cmp.Compare(a.number, b.number)
	})
	var selected []*githubStackChange
	for change := top; change != nil; change = change.projectedBase {
		selected = append(selected, change)
	}
	slices.Reverse(selected)
	return selected, tree.stack, true
}

// githubStackTreeInspection records the existing GitHub state that constrains
// linear-path selection for one projected tree.
type githubStackTreeInspection struct {
	stack     *github.PullRequestStack
	divergent bool
}

// inspectExistingStack finds the one native stack that constrains path
// selection and records whether the projected tree diverges. A tree spanning
// multiple native stacks is ambiguous and is left unchanged.
func (r *githubStackReconciler) inspectExistingStack(
	bottom *githubStackChange,
) (githubStackTreeInspection, bool) {
	var result githubStackTreeInspection
	var remaining ring.Q[*githubStackChange]
	remaining.Push(bottom)
	for !remaining.Empty() {
		change := remaining.Pop()
		if len(r.aboves[change]) > 1 {
			result.divergent = true
		}
		for _, above := range r.aboves[change] {
			remaining.Push(above)
		}

		currentStack := change.pullRequest.stack
		if currentStack == nil {
			continue
		}
		if result.stack == nil {
			result.stack = currentStack
			continue
		}
		if result.stack.Number != currentStack.Number {
			r.warnings = append(r.warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stacks: pull requests belong to different native stacks",
				bottom.number,
			))
			return githubStackTreeInspection{}, false
		}
	}
	return result, true
}

// existingStackPrefix proves that every open member of stack forms one
// base-up path from bottom through the projected tree. The returned change is
// the top of that fixed prefix.
func (r *githubStackReconciler) existingStackPrefix(
	bottom *githubStackChange,
	stack *github.PullRequestStack,
) (*githubStackChange, bool) {
	openMembers := openStackMemberNumbers(stack.Members)
	if len(openMembers) == 0 {
		return nil, false
	}

	var below *githubStackChange
	for i, number := range openMembers {
		change := r.changesByNumber[number]
		if change == nil || change.projection != githubStackProjectionIncluded ||
			(i == 0 && change != bottom) ||
			(i > 0 && change.projectedBase != below) {
			return nil, false
		}
		below = change
	}
	return below, true
}

type githubStackTransitionKind uint8

const (
	githubStackTransitionCurrent githubStackTransitionKind = iota
	githubStackTransitionCreate
	githubStackTransitionAppend
	githubStackTransitionReplace
)

// githubStackTransitionCandidate retains the selected paths and observed stack
// until every desired component has been considered. Candidates that touch the
// same remote stack must be coalesced before their mutations are materialized.
type githubStackTransitionCandidate struct {
	kind        githubStackTransitionKind
	remoteStack *github.PullRequestStack
	paths       [][]*githubStackChange
}

func newGitHubStackTransitionCandidate(
	path []*githubStackChange,
	remoteStack *github.PullRequestStack,
) githubStackTransitionCandidate {
	candidate := githubStackTransitionCandidate{
		remoteStack: remoteStack,
		paths:       [][]*githubStackChange{path},
	}
	if remoteStack == nil {
		candidate.kind = githubStackTransitionCreate
		return candidate
	}

	numbers := stackChangeNumbers(path)
	currentBases := func(changes []*githubStackChange) bool {
		for _, change := range changes {
			if change.baseBranch != "" && change.pullRequest.baseBranch != change.baseBranch {
				return false
			}
		}
		return true
	}
	remoteNumbers := openStackMemberNumbers(remoteStack.Members)
	remoteLength := len(remoteNumbers)
	remoteIsPrefix := remoteLength <= len(numbers) &&
		slices.Equal(remoteNumbers, numbers[:remoteLength])
	switch {
	case remoteLength == len(numbers) && remoteIsPrefix && currentBases(path):
		candidate.kind = githubStackTransitionCurrent
	case remoteLength < len(numbers) && remoteIsPrefix && currentBases(path[:remoteLength]):
		candidate.kind = githubStackTransitionAppend
	default:
		candidate.kind = githubStackTransitionReplace
	}
	return candidate
}

// coalesceReplacements makes one remote stack the unit of mutation. A split
// therefore un-stacks once and recreates every desired component, rather than
// letting component-local transitions race over the same provider object.
func (r *githubStackReconciler) coalesceReplacements(
	candidates []githubStackTransitionCandidate,
) []githubStackTransition {
	byStack := make(map[int]int)
	kept := make([]githubStackTransitionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.remoteStack == nil {
			kept = append(kept, candidate)
			continue
		}
		priorIndex, exists := byStack[candidate.remoteStack.Number]
		if !exists {
			byStack[candidate.remoteStack.Number] = len(kept)
			kept = append(kept, candidate)
			continue
		}
		prior := &kept[priorIndex]
		prior.kind = githubStackTransitionReplace
		prior.paths = append(prior.paths, candidate.paths...)
	}

	var result []githubStackTransition
	for _, candidate := range kept {
		// Replacement is safe only when unstacking can dissolve the provider
		// object completely. Otherwise execution would discover the retained
		// members only after it had already changed remote state.
		replacementBlocked := candidate.kind == githubStackTransitionReplace &&
			slices.ContainsFunc(
				candidate.remoteStack.Members,
				func(member github.PullRequestStackMember) bool {
					return member.Locked ||
						member.State == github.PullRequestStateMerged
				},
			)
		if !replacementBlocked {
			materialized := newGitHubStackTransition(candidate)
			if materialized.unstackNumber != 0 || len(materialized.paths) > 0 {
				result = append(result, materialized)
			}
			continue
		}
		r.warnings = append(r.warnings, fmt.Sprintf(
			"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: merged, queued, or auto-merge pull requests prevent restructuring",
			candidate.paths[0][0].number,
			candidate.remoteStack.Number,
		))
	}
	return result
}

// openStackMemberNumbers projects GitHub's historical stack entries onto the
// active membership used for prefix selection and append operations.
func openStackMemberNumbers(members []github.PullRequestStackMember) []int {
	var numbers []int
	for _, member := range members {
		if member.State == github.PullRequestStateOpen {
			numbers = append(numbers, member.Number)
		}
	}
	return numbers
}

func (r *githubStackReconciler) warnOmittedUpstack(number int, reason string) {
	r.warnings = append(r.warnings, fmt.Sprintf(
		"#%d: Leaving pull request and its upstack out of the GitHub native stack: %s",
		number,
		reason,
	))
}
