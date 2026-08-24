package github

import (
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/gateway/github"
)

// compileGitHubStackProgram compares selected paths with observed membership
// and bases to choose writes and their failure scopes.
// Selections already group paths sharing a remote stack,
// so each replacement has one unstack prerequisite.
func compileGitHubStackProgram(
	selections []githubStackSelection,
) (githubStackProgram, []string) {
	var c githubStackCompiler
	for _, selection := range selections {
		// A selection failure must not prevent later selections from running.
		c.try(func() { c.selection(selection) })
	}
	return c.instructions, c.warnings
}

// githubStackCompiler owns instruction emission for one reconciliation.
// Nested calls to try build failure scopes without executing their mutations.
type githubStackCompiler struct {
	// instructions receives emitted instructions in the current scope.
	// try temporarily replaces it while compiling a nested scope.
	instructions githubStackProgram

	warnings []string
}

// try compiles body into a nested failure scope, omitting empty scopes.
// Warnings belong to the whole compilation and do not change scopes.
func (c *githubStackCompiler) try(body func()) {
	outer := c.instructions
	c.instructions = nil
	body()
	if len(c.instructions) > 0 {
		outer = append(outer, githubStackTry{Body: c.instructions})
	}
	c.instructions = outer
}

// selection emits an append or new membership for the selected paths.
// Incompatible existing membership is dissolved first;
// if GitHub would retain members, it emits only a warning.
func (c *githubStackCompiler) selection(selection githubStackSelection) {
	// GitHub can grow a native stack at its top, but cannot insert, remove,
	// or reorder individual members in place.
	// Members must form a base-to-head chain: each PR above the bottom targets
	// the head branch of the member below it.
	// With member lists ordered bottom-to-top, these changes differ:
	//
	//   [a, b] -> [a, b, c]: append [c].
	//   [a, b] -> [a, c, b]: unstack, align bases, create [a, c, b].
	//
	// Appending c cannot implement the second change: it must continue from
	// the old top b, not insert between a and b.
	if c.appendExisting(selection) {
		return
	}

	if remoteStack := selection.remoteStack; remoteStack != nil {
		// GitHub retains merged and locked members when unstacking.
		// A replacement must dissolve the whole object before any base
		// or new membership write can use the desired topology.
		if slices.ContainsFunc(remoteStack.Members, func(member github.PullRequestStackMember) bool {
			return member.Locked || member.State == github.PullRequestStateMerged
		}) {
			c.warnings = append(c.warnings, fmt.Sprintf(
				"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: merged, queued, or auto-merge pull requests prevent restructuring",
				selection.paths[0][0].number,
				remoteStack.Number,
			))
			return
		}
		c.instructions = append(c.instructions, githubStackUnstack{StackNumber: remoteStack.Number})
	}

	// Unstack shares the selection's scope, so its failure skips all paths.
	// Each path has its own scope so paths can fail independently
	// after the shared prerequisite succeeds.
	for _, selected := range selection.paths {
		var membership githubStackInstruction
		// Splitting [a, b] into separate roots leaves two ordinary PRs.
		// Neither needs new native membership, but b may still need its base
		// moved off a's branch after the old stack has been dissolved.
		if len(selected) >= 2 {
			numbers := make([]int, len(selected))
			for i, change := range selected {
				numbers[i] = change.number
			}
			membership = githubStackCreate{PullRequests: numbers}
		}
		c.path(selected, membership)
	}
}

// appendExisting emits any suffix update for compatible native membership.
// It returns true even if the stack is already current and needs no writes.
// A false result emits nothing, leaving creation or replacement to selection.
func (c *githubStackCompiler) appendExisting(selection githubStackSelection) bool {
	if selection.remoteStack == nil || len(selection.paths) != 1 {
		return false
	}
	selected := selection.paths[0]
	openMembers := openStackMemberNumbers(selection.remoteStack.Members)
	if len(openMembers) > len(selected) {
		return false
	}
	// Matching membership alone does not make a stack appendable in place:
	// GitHub rejects base changes while a PR belongs to a native stack.
	// An existing member needing a new base therefore requires replacement;
	// only the unstacked suffix can be retargeted before appending.
	for i, number := range openMembers {
		change := selected[i]
		if change.number != number ||
			(change.baseBranch != "" && change.pullRequest.BaseRefName != change.baseBranch) {
			return false
		}
	}
	remaining := selected[len(openMembers):]
	if len(remaining) == 0 {
		return true // membership and all bases are already current
	}
	numbers := make([]int, len(remaining))
	for i, change := range remaining {
		numbers[i] = change.number
	}
	c.path(remaining, githubStackAppend{
		StackNumber:  selection.remoteStack.Number,
		PullRequests: numbers,
	})
	return true
}

// path builds a failure scope for bottom-to-top base updates
// followed by membership publication.
// A base-update failure skips the membership write.
// A nil membership produces a base-only update, as needed for a single PR.
func (c *githubStackCompiler) path(
	changes []githubStackChange,
	membership githubStackInstruction,
) {
	c.try(func() {
		for _, change := range changes {
			if change.baseBranch == "" || change.pullRequest.BaseRefName == change.baseBranch {
				continue
			}
			c.instructions = append(c.instructions, githubStackChangeBase{
				PullRequestNumber: change.number,
				PullRequestID:     change.pullRequest.ID,
				BaseBranch:        change.baseBranch,
			})
		}
		if membership != nil {
			c.instructions = append(c.instructions, membership)
		}
	})
}
