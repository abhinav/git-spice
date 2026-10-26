package github

import (
	"fmt"

	"go.abhg.dev/gs/internal/gateway/github"
)

// compileGitHubStackProgram compares selected paths with observed membership
// and bases to choose writes and their failure scopes.
// Groups already collect paths sharing remote stacks,
// so each resource is dissolved at most once before dependent writes.
func compileGitHubStackProgram(
	groups []githubStackGroup,
) (githubStackProgram, []string) {
	var c githubStackCompiler
	for _, group := range groups {
		// A group failure must not prevent later groups from running.
		c.try(func() { c.group(group) })
	}
	return c.instructions, c.warnings
}

// githubStackCompiler owns instruction emission for one reconciliation.
// Group scopes isolate failures without executing their mutations.
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

// group emits base and membership updates for paths sharing resources.
// Compatible native prefixes remain; every other resource is dissolved first.
// Queued or auto-merge members prevent replacement and produce a warning.
func (c *githubStackCompiler) group(group githubStackGroup) {
	// Compile each path independently, but buffer its writes until all shared
	// releases are ready. An unchanged path emits nothing and still retains
	// its resource, which must not be dissolved by the group's release pass.
	var pathPrograms githubStackProgram
	retained := make(map[int]struct{})
	for _, path := range group.paths {
		program, stack := compileGitHubStackPath(path)
		pathPrograms = append(pathPrograms, program...)
		if stack != nil {
			retained[stack.Number] = struct{}{}
		}
	}

	// retained now contains every resource that can remain unchanged;
	// all other remote resources must release their members.
	// Retaining one prefix does not make the rest of its path safe to append:
	// the suffix may still belong to a different resource. For example:
	//
	//   Current: #41 [a, b]    #42 [c, d]
	//   Desired: #41 [a, b, c]    d (ordinary PR, still based on c)
	//   Writes:  unstack #42 → retarget c → append c to #41
	//
	// Only the selected paths authorize base edits and new membership.
	// Dissolving #42 releases d as well, but does not retarget or enroll it.

	// Validate all dissolution prerequisites before emitting any of them.
	// A blocked resource must not leave another resource needlessly dissolved.
	var unstacks []githubStackInstruction
	for _, remoteStack := range group.remoteStacks {
		if _, ok := retained[remoteStack.Number]; ok {
			continue
		}
		// Unstack can leave historical merged members in the old resource.
		// Replacement needs the open PRs released, not that history erased.
		// Execution distinguishes harmless history from unreleased active PRs.
		unstack := githubStackUnstack{StackNumber: remoteStack.Number}
		for _, member := range remoteStack.Members {
			if member.State == github.PullRequestStateMerged {
				unstack.MergedPullRequests = append(unstack.MergedPullRequests, member.Number)
			} else if member.Locked {
				c.warnings = append(c.warnings, fmt.Sprintf(
					"#%d: Leaving pull request and its upstack in existing GitHub native stack #%d: queued or auto-merge pull requests prevent restructuring",
					group.paths[0][0].number,
					remoteStack.Number,
				))
				return
			}
		}
		unstacks = append(unstacks, unstack)
	}
	c.instructions = append(c.instructions, unstacks...)

	// All releases must succeed before any path can retarget or join a stack.
	// Nest each path's scope inside the shared group scope:
	//
	//   TRY
	//       UNSTACK resources being replaced
	//       TRY: base edits, then membership for path A
	//       TRY: base edits, then membership for path B
	//
	// Failed release skips both paths; failed path A still permits path B.
	// Successful writes are not rolled back when a later instruction fails.
	c.instructions = append(c.instructions, pathPrograms...)
}

// compileGitHubStackPath builds one path's failure scope and identifies
// the native resource it can retain, if any.
// Changes must be a nonempty, eligible path ordered bottom-to-top.
// The program assumes the group's incompatible resources have been released.
// A base-update failure skips the membership write.
// A path needing no writes returns a nil program but may still retain a resource.
func compileGitHubStackPath(
	changes []githubStackChange,
) (githubStackProgram, *github.PullRequestStack) {
	// A retained resource must begin at this path's bottom PR.
	// Each PR belongs to at most one native stack, so the bottom's observed
	// stack is the only candidate; there is no search across other paths.
	// Paths are disjoint, so two paths cannot retain the same resource.
	var retained *github.PullRequestStack
	var prefixLen int
	if stack := changes[0].pullRequest.Stack; stack != nil {
		// GitHub can grow a native stack at its top, but cannot insert, remove,
		// or reorder individual members in place.
		// Open members form a base-to-head chain:
		// each PR above the first open member targets the head of the one below it.
		// With member lists ordered bottom-to-top, these updates differ:
		//
		//   [a, b] → [a, b, c]: append [c].
		//   [a, b] → [a, c, b]: unstack, align bases, create [a, c, b].
		//
		// Appending c cannot implement insertion: it must continue from the old
		// top b. Even a no-op base write on a native member is rejected, so a
		// retained prefix must match both desired membership and desired bases.
		//
		// Merged members are history outside the open base chain.
		// Closed members still occupy their native positions and block merging
		// the open suffix. Release them before rebuilding membership; filtering
		// them out could also append after the wrong top PR.
		compatible := true
		var matched int
		for _, member := range stack.Members {
			if member.State == github.PullRequestStateMerged {
				continue
			}
			if member.State != github.PullRequestStateOpen || matched == len(changes) {
				compatible = false
				break
			}
			change := changes[matched]
			if change.number != member.Number ||
				(change.baseBranch != "" && change.pullRequest.BaseRefName != change.baseBranch) {
				compatible = false
				break
			}
			matched++
		}
		if compatible && matched > 0 {
			retained = stack
			prefixLen = matched
		}
	}
	remaining := changes[prefixLen:]
	if len(remaining) == 0 {
		return nil, retained // membership and all bases are already current
	}

	// Only the suffix beyond a retained prefix needs writes.
	// Without a retained resource, remaining is the entire desired path.
	var body githubStackProgram
	for _, change := range remaining {
		if change.baseBranch == "" || change.pullRequest.BaseRefName == change.baseBranch {
			continue
		}
		body = append(body, githubStackChangeBase{
			PullRequestNumber: change.number,
			PullRequestID:     change.pullRequest.ID,
			BaseBranch:        change.baseBranch,
		})
	}

	// A single PR without a retained prefix needs no native resource,
	// but may still need its base changed after its old stack was dissolved.
	if retained != nil || len(remaining) >= 2 {
		numbers := make([]int, len(remaining))
		for i, change := range remaining {
			numbers[i] = change.number
		}
		if retained != nil {
			body = append(body, githubStackAppend{StackNumber: retained.Number, PullRequests: numbers})
		} else {
			body = append(body, githubStackCreate{PullRequests: numbers})
		}
	}
	if len(body) == 0 {
		return nil, retained
	}
	return githubStackProgram{githubStackTry{Body: body}}, retained
}
