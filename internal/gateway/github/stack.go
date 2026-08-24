package github

import (
	"context"
	"fmt"
)

// PullRequestStack describes a native pull request stack.
type PullRequestStack struct {
	// Number is the number GitHub assigned to this stack.
	// It identifies the stack within its repository and is unrelated to member
	// pull request numbers or entry positions.
	Number int

	// Members lists every stack member from the base upward.
	// Merged members remain present because GitHub cannot remove them when a
	// stack is dissolved.
	Members []PullRequestStackMember
}

// PullRequestStackMember describes one member of a native stack.
type PullRequestStackMember struct {
	// Number is the repository-local pull request number.
	Number int

	// State is the pull request lifecycle state.
	State PullRequestState

	// Locked reports whether GitHub must preserve this member because it is in
	// a merge queue or has auto-merge enabled.
	Locked bool
}

// CheckPullRequestStacks verifies that the repository exposes GitHub's native
// stack REST API without changing repository state.
// See https://docs.github.com/en/rest/pulls/stacks#list-pull-request-stacks.
func (c *Gateway) CheckPullRequestStacks(
	ctx context.Context,
	owner string,
	repo string,
) error {
	if err := c.getREST(ctx, []string{"repos", owner, repo, "stacks"}, nil); err != nil {
		return fmt.Errorf("check pull request stacks: %w", err)
	}
	return nil
}
