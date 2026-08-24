package github

import "go.abhg.dev/gs/internal/gateway/github"

// githubStackInstruction is one mutation or failure scope in a stack program.
// Instructions are immutable values constructed by compileGitHubStackProgram.
type githubStackInstruction interface {
	isGitHubStackInstruction()
}

// githubStackProgram orders stack mutations and their failure dependencies.
// Ordinary sequences stop at the first error;
// githubStackTry scopes allow independent work to continue.
type githubStackProgram []githubStackInstruction

// githubStackTry records the first uncaught error in Body,
// skips the rest of Body, and resumes with the following instruction.
// It does not retry writes or recover panics.
type githubStackTry struct {
	Body []githubStackInstruction
}

func (githubStackTry) isGitHubStackInstruction() {}

// githubStackUnstack requires complete dissolution of an existing native stack.
// Retained members count as failure even when the API call succeeds.
type githubStackUnstack struct {
	// StackNumber identifies the existing stack within the repository.
	StackNumber int
}

func (githubStackUnstack) isGitHubStackInstruction() {}

// githubStackChangeBase retargets one PR to its desired base branch.
type githubStackChangeBase struct {
	// PullRequestNumber identifies the PR in diagnostics.
	PullRequestNumber int

	// PullRequestID identifies the PR for the GraphQL mutation.
	PullRequestID github.ID

	// BaseBranch is the desired provider-facing base branch.
	BaseBranch string
}

func (githubStackChangeBase) isGitHubStackInstruction() {}

// githubStackCreate creates a native stack from the supplied PRs.
type githubStackCreate struct {
	// PullRequests lists all members in bottom-to-top order.
	PullRequests []int
}

func (githubStackCreate) isGitHubStackInstruction() {}

// githubStackAppend extends an existing native stack.
type githubStackAppend struct {
	// StackNumber identifies the existing stack within the repository.
	StackNumber int

	// PullRequests lists only new members in bottom-to-top order,
	// excluding the existing stack prefix.
	PullRequests []int
}

func (githubStackAppend) isGitHubStackInstruction() {}
