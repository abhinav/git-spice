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
	groups, selectionWarnings := forest.selectChangeTrees()
	program, programWarnings := compileGitHubStackProgram(groups)
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

// githubStackGroup collects desired base-up paths
// connected by their observed native stacks.
// All paths share the group's dissolution prerequisites;
// after those succeed, each path can be updated independently.
type githubStackGroup struct {
	// remoteStacks contains the observed native stacks touched by these paths,
	// including members outside the request. A path may join several old stacks;
	// several paths may split one old stack. Shared resources keep their writes
	// in one failure scope so dissolution happens once, before dependent writes.
	remoteStacks []*github.PullRequestStack

	// paths contains one complete selected linear chain per projected tree,
	// with each chain ordered bottom-to-top.
	// Paths are nonempty and disjoint: a PR appears in at most one path.
	//
	// Multiple paths occur when desired trees split one existing remote stack:
	// existing [a, b, c, d] becomes desired [a, b] and [c, d].
	// These are separate desired chains sharing one unstack prerequisite,
	// after which each chain can be updated independently.
	//
	// One path may also join several remote stacks:
	// existing [a, b] and [c, d] become desired [a, b, c].
	// The compiler can retain [a, b], but must release c from [c, d] first.
	// With no remote stacks, there is only one path.
	paths [][]githubStackChange
}

// selectChangeTrees chooses one linear path per projected tree
// and groups paths sharing resources around their dissolution prerequisites.
// A tree rejected for incompatible native membership protects its stacks
// from updates by other trees.
// Omitted trees and divergent upstacks produce warnings.
func (f *githubStackForest) selectChangeTrees() ([]githubStackGroup, []string) {
	var candidates []githubStackGroup
	var warnings []string
	protectedStacks := make(map[int]struct{})

	// Each bottom owns a complete projected tree, not just newly submitted PRs.
	// A split can produce several selected paths touching one native stack:
	//
	//   Current #42: #1 ─→ #2 ─→ #3 ─→ #4
	//   Desired:     #1 ─→ #2
	//                #3 ─→ #4
	//
	// Conversely, one desired path may touch several current stacks.
	// Path selection settles the desired chains; grouping then gives every
	// connected set its shared prerequisites before any dependent base edits.
	for _, bottom := range f.bottoms {
		selected, remoteStacks, warning := f.selectLinearPath(bottom)
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

		candidates = append(candidates, githubStackGroup{
			remoteStacks: remoteStacks,
			paths:        [][]githubStackChange{selected},
		})
	}
	// Only settle shared-stack writes after every tree has had a chance
	// to protect its existing membership.
	var updates []githubStackGroup
	for _, group := range groupGitHubStackPaths(candidates) {
		var protected *github.PullRequestStack
		for _, stack := range group.remoteStacks {
			if _, ok := protectedStacks[stack.Number]; ok {
				protected = stack
				break
			}
		}
		if protected == nil {
			updates = append(updates, group)
			continue
		}
		for _, path := range group.paths {
			warnings = append(warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: another change tree sharing this stack cannot be reconciled",
				path[0].number,
				protected.Number,
			))
		}
	}
	return updates, warnings
}

// groupGitHubStackPaths collects paths connected through native resources.
// A path can join several old stacks, and another path can split any of them.
// Those paths must share every dissolution prerequisite, including connections
// reached indirectly through another path, so each resource is released once.
func groupGitHubStackPaths(candidates []githubStackGroup) []githubStackGroup {
	// Paths and resources form a second graph, distinct from PR base edges.
	// Here a line means "path touches resource", not "is based on":
	//
	//   #41 ── path A ── #42 ── path B ── #43
	//
	// A and B must share a failure scope even though they touch different
	// outer resources. Releasing #42 twice would fail; updating either path
	// before all required releases succeed could leave native membership
	// blocking its base edits. byStack lets the walk cross from each resource
	// to every path that depends on it, including these indirect connections.
	byStack := make(map[int][]int)
	for index, candidate := range candidates {
		for _, stack := range candidate.remoteStacks {
			byStack[stack.Number] = append(byStack[stack.Number], index)
		}
	}

	var groups []githubStackGroup
	seen := make(map[int]struct{})
	for index := range candidates {
		if _, ok := seen[index]; ok {
			continue
		}

		// Seed a new group with a path not reached by an earlier walk.
		seen[index] = struct{}{}
		var remaining ring.Q[int]
		remaining.Push(index)

		var group githubStackGroup
		stacks := make(map[int]struct{})
		for !remaining.Empty() {
			candidate := candidates[remaining.Pop()]
			group.paths = append(group.paths, candidate.paths...)

			for _, stack := range candidate.remoteStacks {
				if _, ok := stacks[stack.Number]; ok {
					continue
				}
				stacks[stack.Number] = struct{}{}
				group.remoteStacks = append(group.remoteStacks, stack)

				// Follow this resource to paths not yet queued in this group.
				for _, connected := range byStack[stack.Number] {
					if _, ok := seen[connected]; !ok {
						seen[connected] = struct{}{}
						remaining.Push(connected)
					}
				}
			}
		}

		groups = append(groups, group)
	}
	return groups
}

// selectLinearPath returns one desired base-up path
// and the existing native stacks it touches, if any.
// A nonempty warning means the tree was omitted.
// Divergent trees must preserve a compatible existing prefix;
// a linear tree may replace incompatible membership.
func (f *githubStackForest) selectLinearPath(
	bottom int,
) ([]githubStackChange, []*github.PullRequestStack, string) {
	var remoteStacks []*github.PullRequestStack
	seenStacks := make(map[int]struct{})
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
		if _, ok := seenStacks[currentStack.Number]; !ok {
			seenStacks[currentStack.Number] = struct{}{}
			remoteStacks = append(remoteStacks, currentStack)
		}
	}
	if divergent && len(remoteStacks) > 1 {
		return nil, nil, fmt.Sprintf(
			"#%d: Leaving pull request and its upstack in existing GitHub native stacks: pull requests belong to different native stacks",
			bottom,
		)
	}

	// In a linear tree, replacing several resources cannot choose one sibling
	// over another. A divergent tree has no such unambiguous replacement:
	// it must preserve the one existing resource or leave the tree unchanged.
	extensionBase := bottom
	if len(remoteStacks) > 0 {
		remoteStack := remoteStacks[0]
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
	return selected, remoteStacks, ""
}

// openStackMemberNumbers returns open member PR numbers in stack order
// for path selection. Closed members still require release before executing
// that path, even when its open members form a compatible prefix.
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
