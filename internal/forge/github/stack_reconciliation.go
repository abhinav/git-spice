package github

import (
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
// Changes must be unique and ordered base-first, with bases outside the request
// represented by zero. The input records and their observed GitHub state are
// never mutated.
func reconcileGitHubStacks(changes []githubStackChange) githubStackReconciliation {
	reconciler := githubStackReconciler{
		changesByNumber: make(map[int]githubStackChange, len(changes)),
		baseByNumber:    make(map[int]int, len(changes)),
		aboves:          make(map[int][]int, len(changes)),
	}
	for _, change := range changes {
		reconciler.changesByNumber[change.number] = change
	}

	// Projection establishes the eligible forest. Selection preserves existing
	// native membership and groups paths that share one remote stack before
	// planning any replacement of that stack.
	errs := reconciler.projectChangeTrees(changes)
	selections := reconciler.selectChangeTrees()
	transitions := reconciler.planTransitions(selections)
	return githubStackReconciliation{
		transitions: transitions,
		warnings:    reconciler.warnings,
		errs:        errs,
	}
}

// githubStackReconciler owns one invocation's projected forest and warnings.
// baseByNumber contains only eligible open pull requests; a zero base means a
// root. aboves and bottoms are derived from those projected relationships.
type githubStackReconciler struct {
	changesByNumber map[int]githubStackChange
	baseByNumber    map[int]int
	aboves          map[int][]int
	bottoms         []int

	warnings []string
}

// projectChangeTrees removes relationships GitHub cannot represent. Closed or
// merged changes are transparent; missing or cross-repository changes exclude
// their complete requested upstack. Missing changes also produce errors.
func (r *githubStackReconciler) projectChangeTrees(changes []githubStackChange) []error {
	var errs []error

	// Each visited, non-excluded change records the nearest included PR at or
	// below it. Zero means there is none. An absent entry means the change was
	// excluded, so its upstack must also be excluded. Base-first order makes
	// that distinction final before an above change is visited.
	nearestIncluded := make(map[int]int, len(changes))
	for _, change := range changes {
		var below int
		if change.baseNumber != 0 {
			var ok bool
			below, ok = nearestIncluded[change.baseNumber]
			if !ok {
				continue
			}
		}

		pullRequest := change.pullRequest
		if pullRequest == nil {
			errs = append(errs, fmt.Errorf(
				"inspect GitHub pull request #%d: %w",
				change.number,
				forge.ErrNotFound,
			))
			r.warnOmittedUpstack(change.number, "the pull request was not found")
			continue
		}
		if pullRequest.State != github.PullRequestStateOpen {
			nearestIncluded[change.number] = below
			continue
		}
		if !change.headInRepository {
			r.warnOmittedUpstack(
				change.number,
				"the head branch is not in the receiving repository",
			)
			continue
		}

		nearestIncluded[change.number] = change.number
		r.baseByNumber[change.number] = below
		if below == 0 {
			r.bottoms = append(r.bottoms, change.number)
		} else {
			r.aboves[below] = append(r.aboves[below], change.number)
		}
	}

	// Keep independent-tree processing and divergent-upstack warnings stable.
	slices.Sort(r.bottoms)
	for _, aboves := range r.aboves {
		slices.Sort(aboves)
	}
	return errs
}

// githubStackSelection groups selected base-up paths that touch the same
// existing native stack. A nil remoteStack identifies a new, independent stack.
type githubStackSelection struct {
	remoteStack *github.PullRequestStack
	paths       [][]int
}

func (r *githubStackReconciler) selectChangeTrees() []githubStackSelection {
	var selections []githubStackSelection
	byStack := make(map[int]int)

	// Each bottom owns a complete projected tree, not just newly submitted PRs.
	// Its selected path includes any existing native prefix. A stack split can
	// produce several selected paths touching the same provider object; retain
	// them together so execution dissolves that object only once.
	for _, bottom := range r.bottoms {
		selected, remoteStack, ok := r.selectLinearPath(bottom)
		if !ok {
			continue
		}
		for idx, number := range selected {
			var selectedAbove int
			if idx+1 < len(selected) {
				selectedAbove = selected[idx+1]
			}
			for _, above := range r.aboves[number] {
				if above != selectedAbove {
					r.warnOmittedUpstack(
						above,
						"the change tree diverges from the selected linear path",
					)
				}
			}
		}

		if remoteStack != nil {
			if index, exists := byStack[remoteStack.Number]; exists {
				selections[index].paths = append(selections[index].paths, selected)
				continue
			}
			byStack[remoteStack.Number] = len(selections)
		}
		selections = append(selections, githubStackSelection{
			remoteStack: remoteStack,
			paths:       [][]int{selected},
		})
	}
	return selections
}

// selectLinearPath returns one desired base-up path and the existing native
// stack it touches, if any. False means selection warned and omitted the tree.
// Divergent trees must preserve a compatible existing prefix; a linear tree
// may replace incompatible membership.
func (r *githubStackReconciler) selectLinearPath(
	bottom int,
) ([]int, *github.PullRequestStack, bool) {
	var remoteStack *github.PullRequestStack
	var divergent bool
	var remaining ring.Q[int]
	remaining.Push(bottom)
	for !remaining.Empty() {
		number := remaining.Pop()
		aboves := r.aboves[number]
		if len(aboves) > 1 {
			divergent = true
		}
		for _, above := range aboves {
			remaining.Push(above)
		}

		currentStack := r.changesByNumber[number].pullRequest.Stack
		if currentStack == nil {
			continue
		}
		if remoteStack == nil {
			remoteStack = currentStack
			continue
		}
		if remoteStack.Number != currentStack.Number {
			r.warnings = append(r.warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stacks: pull requests belong to different native stacks",
				bottom,
			))
			return nil, nil, false
		}
	}

	extensionBase := bottom
	if remoteStack != nil {
		// Every open native member must form one prefix from this bottom.
		// Only a compatible prefix can constrain which divergent path we select;
		// a linear tree can instead replace incompatible composition.
		openMembers := openStackMemberNumbers(remoteStack.Members)
		compatible := len(openMembers) > 0
		var prefixTop int
		for i, number := range openMembers {
			base, included := r.baseByNumber[number]
			if !included || (i == 0 && number != bottom) ||
				(i > 0 && base != prefixTop) {
				compatible = false
				break
			}
			prefixTop = number
		}

		if !compatible && divergent {
			formatted := make([]string, len(openMembers))
			for i, number := range openMembers {
				formatted[i] = fmt.Sprintf("#%d", number)
			}
			r.warnings = append(r.warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: open pull requests %s have incompatible membership",
				bottom,
				remoteStack.Number,
				strings.Join(formatted, ", "),
			))
			return nil, nil, false
		}
		if compatible {
			extensionBase = prefixTop
		}
	}

	// Choose only above the fixed native prefix. FurthestChildren supplies the
	// maximum depth; the lowest top PR number breaks ties deterministically.
	tops := graph.FurthestChildren(extensionBase, func(number int) []int {
		return r.aboves[number]
	})
	var selected []int
	for number := slices.Min(tops); number != 0; number = r.baseByNumber[number] {
		selected = append(selected, number)
	}
	slices.Reverse(selected)
	return selected, remoteStack, true
}

// planTransitions compares complete selected compositions with observed
// membership and bases, then emits concrete writes. Grouping has already made
// each remote stack one unit of replacement.
func (r *githubStackReconciler) planTransitions(
	selections []githubStackSelection,
) []githubStackTransition {
	var transitions []githubStackTransition
	for _, selection := range selections {
		var transition githubStackTransition
		var stackNumber, start int
		if remoteStack := selection.remoteStack; remoteStack != nil {
			remoteNumbers := openStackMemberNumbers(remoteStack.Members)
			remoteLength := len(remoteNumbers)

			// A single path can retain existing membership only when its bases
			// still agree with GitHub. Multiple paths require splitting the
			// existing stack regardless of their individual prefix matches.
			if len(selection.paths) == 1 {
				path := selection.paths[0]
				prefixMatches := remoteLength <= len(path) &&
					slices.Equal(remoteNumbers, path[:remoteLength])
				basesMatch := prefixMatches
				if prefixMatches {
					for _, number := range path[:remoteLength] {
						change := r.changesByNumber[number]
						if change.baseBranch != "" && change.pullRequest.BaseRefName != change.baseBranch {
							basesMatch = false
							break
						}
					}
				}
				if basesMatch {
					if remoteLength == len(path) {
						continue // membership and all bases are already current
					}
					stackNumber = remoteStack.Number
					start = remoteLength
				}
			}

			if stackNumber == 0 {
				// GitHub retains merged and locked members when unstacking.
				// A replacement must dissolve the whole object before any base
				// or new membership write can use the desired topology.
				if slices.ContainsFunc(remoteStack.Members, func(member github.PullRequestStackMember) bool {
					return member.Locked || member.State == github.PullRequestStateMerged
				}) {
					r.warnings = append(r.warnings, fmt.Sprintf(
						"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: merged, queued, or auto-merge pull requests prevent restructuring",
						selection.paths[0][0],
						remoteStack.Number,
					))
					continue
				}
				transition.unstackNumber = remoteStack.Number
			}
		}

		// Retarget each path bottom-to-top before publishing membership. Append
		// plans skip the existing prefix; create and replacement plans use the
		// complete selected path. Single PRs can need retargeting without a stack.
		for _, selected := range selection.paths {
			var path githubStackPathTransition
			for _, number := range selected[start:] {
				change := r.changesByNumber[number]
				if change.baseBranch == "" || change.pullRequest.BaseRefName == change.baseBranch {
					continue
				}
				path.baseUpdates = append(path.baseUpdates, githubPullRequestBaseUpdate{
					pullRequestNumber: number,
					pullRequestID:     change.pullRequest.ID,
					baseBranch:        change.baseBranch,
				})
			}
			if len(selected) >= 2 {
				path.stackNumber = stackNumber
				path.pullRequests = slices.Clone(selected[start:])
			}
			if len(path.baseUpdates) > 0 || len(path.pullRequests) > 0 {
				transition.paths = append(transition.paths, path)
			}
		}
		if transition.unstackNumber != 0 || len(transition.paths) > 0 {
			transitions = append(transitions, transition)
		}
	}
	return transitions
}

// openStackMemberNumbers projects historical stack entries onto the active
// membership used for prefix selection and append operations.
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
