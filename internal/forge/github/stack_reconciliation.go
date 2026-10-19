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

// githubStackReconciliation is the result of reconciling
// one desired forest with its observed GitHub state.
type githubStackReconciliation struct {
	program  githubStackProgram
	warnings []string
	errs     []error
}

// reconcileGitHubStacks plans the GitHub mutations needed
// to represent the requested base-up relationships.
//
// Changes must be unique and ordered base-first,
// with bases outside the request represented by zero.
// The input records and their observed GitHub state are never mutated.
func reconcileGitHubStacks(changes []githubStackChange) githubStackReconciliation {
	// A PR's base branch and its native stack membership are separate facts.
	// The base branch is the PR's merge target; native membership is an explicit,
	// ordered GitHub resource, not a view recomputed from PR base branches.
	// Our desired relationships can fork or split an existing native stack,
	// while each GitHub stack must remain linear.
	// Selecting native membership therefore need not include every desired PR,
	// and omitting a PR does not authorize changing its requested base.
	forest, projectionWarnings, errs := projectChangeTrees(changes)
	selections, selectionWarnings := forest.selectChangeTrees()
	program, programWarnings := compileGitHubStackProgram(selections)
	return githubStackReconciliation{
		program: program,
		warnings: slices.Concat(
			projectionWarnings,
			selectionWarnings,
			programWarnings,
		),
		errs: errs,
	}
}

// githubStackForest is the eligible topology used to select native stacks.
// All indexes refer to eligible open PRs in changesByNumber.
// Construction settles the topology; selection only reads it.
type githubStackForest struct {
	// changesByNumber retains the immutable requested relationships
	// and observed GitHub facts for participating PRs.
	changesByNumber map[int]githubStackChange

	// baseByNumber maps each PR to its projected base, or zero for a root.
	// Unlike the requested base, this may skip closed or merged PRs.
	// A root has no included base; its base branch need not be trunk.
	baseByNumber map[int]int

	// aboves lists direct projected aboves, ordered by PR number.
	aboves map[int][]int

	// bottoms lists projected roots, ordered by PR number.
	bottoms []int
}

// projectChangeTrees builds an eligible forest without mutating changes.
// Closed or merged changes are omitted without excluding their upstack.
// Missing or cross-repository changes exclude their entire upstack.
// Missing changes also produce errors; unaffected trees remain in the forest.
func projectChangeTrees(
	changes []githubStackChange,
) (*githubStackForest, []string, []error) {
	forest := &githubStackForest{
		changesByNumber: make(map[int]githubStackChange, len(changes)),
		baseByNumber:    make(map[int]int, len(changes)),
		aboves:          make(map[int][]int, len(changes)),
	}
	var warnings []string
	var errs []error

	// An open, eligible PR records itself as the nearest included PR.
	// Closed or merged PRs are omitted from membership,
	// but record the nearest included PR below them (zero if there is none).
	// An above PR can connect through them to the included base
	// when its requested base branch matches that included PR's head.
	//
	// With arrows pointing from base to above:
	//
	//   Requested: a(open) ─→ b(merged) ─→ c(open)
	//   Projected: a ────────────────────→ c
	//
	// Missing or cross-repository PRs leave no nearestIncluded entry,
	// so the failed lookup excludes their entire upstack.
	//
	// Processing bases before aboves settles each entry
	// before an above PR looks it up.
	nearestIncluded := make(map[int]int, len(changes))
	for _, c := range changes {
		var below int

		if base := c.baseNumber; base != 0 {
			var ok bool
			below, ok = nearestIncluded[base]
			if !ok {
				continue
			}
		}

		pr := c.pullRequest
		if pr == nil {
			errs = append(errs, fmt.Errorf(
				"inspect GitHub pull request #%d: %w", c.number, forge.ErrNotFound,
			))
			warnings = append(warnings, omittedUpstackWarning(
				c.number, "the pull request was not found",
			))
			continue
		}
		if pr.State != github.PullRequestStateOpen {
			nearestIncluded[c.number] = below
			continue
		}

		// Native membership requires head branches in the receiving repository.
		if !c.headInRepository {
			warnings = append(warnings, omittedUpstackWarning(
				c.number, "the head branch is not in the receiving repository",
			))
			continue
		}

		// Closing or merging a PR makes it ineligible for new membership,
		// but does not change the requested base of the PR above it.
		// In a -> b(closed) -> c, c may still deliberately target b's branch.
		// Joining a and c would require c to target a's head instead;
		// use that projected edge only when baseBranch already requests it.
		if below != 0 && below != c.baseNumber &&
			c.baseBranch != forest.changesByNumber[below].pullRequest.HeadRefName {
			below = 0
		}

		nearestIncluded[c.number] = c.number
		forest.changesByNumber[c.number] = c
		forest.baseByNumber[c.number] = below
		if below == 0 {
			forest.bottoms = append(forest.bottoms, c.number)
		} else {
			forest.aboves[below] = append(forest.aboves[below], c.number)
		}
	}

	// Keep processing and divergent-branch warnings deterministic.
	slices.Sort(forest.bottoms)
	for _, aboves := range forest.aboves {
		slices.Sort(aboves)
	}
	return forest, warnings, errs
}

// githubStackSelection groups desired base-up paths
// that touch the same observed native stack.
type githubStackSelection struct {
	// remoteStack is the observed native stack,
	// including its current membership.
	//
	// It is nil for a new, independent stack.
	remoteStack *github.PullRequestStack

	// paths contains one complete selected linear chain per projected tree,
	// with each chain ordered bottom-to-top.
	//
	// Multiple paths occur when desired trees split one existing remoteStack:
	// existing [a, b, c, d] becomes desired [a, b] and [c, d].
	// These are separate desired chains sharing one unstack prerequisite,
	// after which each chain can be updated independently.
	//
	// With no remoteStack, there is only one path.
	paths [][]githubStackChange
}

// selectChangeTrees chooses one linear path per projected tree
// and groups paths sharing an existing stack around one unstack prerequisite.
// A tree rejected for incompatible native membership protects its stacks
// from updates by other trees.
// Omitted trees and divergent upstacks produce warnings.
func (f *githubStackForest) selectChangeTrees() ([]githubStackSelection, []string) {
	var selections []githubStackSelection
	var warnings []string
	byStack := make(map[int]int)
	protectedStacks := make(map[int]struct{})

	// Each bottom owns a complete projected tree, not just newly submitted PRs.
	// A split can produce several selected paths touching one native stack:
	//
	//   Current #42: #1 ─→ #2 ─→ #3 ─→ #4
	//   Desired:     #1 ─→ #2
	//                #3 ─→ #4
	//
	// Group those paths by remote stack so they share one unstack prerequisite
	// before either path can retarget bases and publish membership.
	for _, bottom := range f.bottoms {
		selected, remoteStack, warning := f.selectLinearPath(bottom)
		if warning != "" {
			warnings = append(warnings, warning)
			// Desired trees are disjoint, but unstack mutates a whole observed stack.
			// Leaving this tree unchanged therefore protects every stack it touches
			// from writes by other trees, even ones selected earlier.
			// Scan the whole tree: path selection may stop at its first conflict.
			var remaining ring.Q[int]
			remaining.Push(bottom)
			for !remaining.Empty() {
				number := remaining.Pop()
				if stack := f.changesByNumber[number].pullRequest.Stack; stack != nil {
					protectedStacks[stack.Number] = struct{}{}
				}
				for _, above := range f.aboves[number] {
					remaining.Push(above)
				}
			}
			continue
		}
		for idx, change := range selected {
			var selectedAbove int
			if idx+1 < len(selected) {
				selectedAbove = selected[idx+1].number
			}
			for _, above := range f.aboves[change.number] {
				if above != selectedAbove {
					warnings = append(warnings, omittedUpstackWarning(
						above,
						"the change tree diverges from the selected linear path",
					))
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
			paths:       [][]githubStackChange{selected},
		})
	}
	// Only settle shared-stack writes after every tree has had a chance
	// to protect its existing membership.
	var updates []githubStackSelection
	for _, selection := range selections {
		stack := selection.remoteStack
		if stack == nil {
			updates = append(updates, selection)
			continue
		}
		if _, protected := protectedStacks[stack.Number]; !protected {
			updates = append(updates, selection)
			continue
		}
		for _, path := range selection.paths {
			warnings = append(warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: another change tree sharing this stack cannot be reconciled",
				path[0].number,
				stack.Number,
			))
		}
	}
	return updates, warnings
}

// selectLinearPath returns one desired base-up path
// and the existing native stack it touches, if any.
// A nonempty warning means the tree was omitted.
// Divergent trees must preserve a compatible existing prefix;
// a linear tree may replace incompatible membership.
func (f *githubStackForest) selectLinearPath(
	bottom int,
) ([]githubStackChange, *github.PullRequestStack, string) {
	var remoteStack *github.PullRequestStack
	var divergent bool

	// Inspect the whole tree before choosing a path.
	// Native membership on any above can constrain which path is preserved.
	var remaining ring.Q[int]
	remaining.Push(bottom)
	for !remaining.Empty() {
		number := remaining.Pop()
		aboves := f.aboves[number]
		if len(aboves) > 1 {
			divergent = true
		}
		for _, above := range aboves {
			remaining.Push(above)
		}

		currentStack := f.changesByNumber[number].pullRequest.Stack
		if currentStack == nil {
			continue
		}
		if remoteStack == nil {
			remoteStack = currentStack
			continue
		}
		if remoteStack.Number != currentStack.Number {
			return nil, nil, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stacks: pull requests belong to different native stacks",
				bottom,
			)
		}
	}

	extensionBase := bottom
	if remoteStack != nil {
		// Existing membership is compatible only when all its open members
		// form a prefix starting at bottom.
		// Preserve that prefix before choosing its longest extension:
		//
		//                 ┌─→ #3 ─→ #5
		//   #1 ─→ #2 ─────┤
		//                 └─→ #4
		//
		// If native membership is #1 → #2 → #4, select that shorter path
		// rather than replace it with #1 → #2 → #3 → #5.
		// Without a compatible prefix in a divergent tree, selecting a path
		// would evict existing native membership in favor of one sibling.
		// Leave that choice alone; a linear tree has no competing sibling path
		// and can instead proceed toward replacement.
		openMembers := openStackMemberNumbers(remoteStack.Members)
		compatible := len(openMembers) > 0
		var prefixTop int
		for i, number := range openMembers {
			base, included := f.baseByNumber[number]
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
			return nil, nil, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: open pull requests %s have incompatible membership",
				bottom,
				remoteStack.Number,
				strings.Join(formatted, ", "),
			)
		}
		if compatible {
			extensionBase = prefixTop
		}
	}

	// Each eligible PR has at most one included base, but can have several aboves.
	// The search can therefore fork upwards, while any chosen top has exactly
	// one downstack path back to its bottom.
	// Once extensionBase is fixed, all candidate paths share its downstack,
	// so the furthest top also gives the longest complete path.
	// Break ties by the lowest top PR number, then walk to the bottom
	// to include that prefix as well as its extension.
	tops := graph.FurthestChildren(extensionBase, func(number int) []int {
		return f.aboves[number]
	})
	var selected []githubStackChange
	for number := slices.Min(tops); number != 0; number = f.baseByNumber[number] {
		selected = append(selected, f.changesByNumber[number])
	}
	slices.Reverse(selected)
	return selected, remoteStack, ""
}

// openStackMemberNumbers returns open member PR numbers in stack order
// for prefix selection and append operations.
func openStackMemberNumbers(members []github.PullRequestStackMember) []int {
	var numbers []int
	for _, member := range members {
		if member.State == github.PullRequestStateOpen {
			numbers = append(numbers, member.Number)
		}
	}
	return numbers
}

func omittedUpstackWarning(number int, reason string) string {
	return fmt.Sprintf(
		"#%d: Leaving pull request and its upstack out of the GitHub native stack: %s",
		number,
		reason,
	)
}
