package github

import (
	"context"
	"fmt"
	"strconv"
)

const maxStackPullRequests = 100

// UnstackPullRequestStackInput identifies a native stack to dissolve.
type UnstackPullRequestStackInput struct {
	// Owner is the login that owns the repository.
	Owner string // required

	// Repo is the repository name.
	Repo string // required

	// StackNumber identifies the stack within the repository.
	StackNumber int // required
}

// UnstackPullRequestStackResult reports members GitHub kept stacked. An empty
// result means that GitHub dissolved the complete stack.
type UnstackPullRequestStackResult struct {
	RemainingPullRequests []int
}

// UnstackPullRequestStack dissolves a native pull request stack. GitHub may
// preserve merged, queued, or auto-merge members and return them in a smaller
// stack.
// See https://docs.github.com/en/rest/pulls/stacks#remove-pull-requests-from-a-pull-request-stack.
func (c *Gateway) UnstackPullRequestStack(
	ctx context.Context,
	input *UnstackPullRequestStackInput,
) (*UnstackPullRequestStackResult, error) {
	var res struct {
		PullRequests []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	}
	if err := c.postREST(
		ctx,
		[]string{
			"repos", input.Owner, input.Repo, "stacks",
			strconv.Itoa(input.StackNumber),
			"unstack",
		},
		nil,
		&res,
	); err != nil {
		return nil, fmt.Errorf("unstack pull request stack: %w", err)
	}

	result := &UnstackPullRequestStackResult{
		RemainingPullRequests: make([]int, len(res.PullRequests)),
	}
	for i, pullRequest := range res.PullRequests {
		result.RemainingPullRequests[i] = pullRequest.Number
	}
	return result, nil
}

// CreatePullRequestStackInput identifies the pull requests for a new stack.
type CreatePullRequestStackInput struct {
	// Owner is the login that owns the repository.
	Owner string // required

	// Repo is the repository name.
	Repo string // required

	// PullRequests lists pull request numbers from the base upward.
	// GitHub accepts between 2 and 100 members.
	PullRequests []int // required
}

// CreatePullRequestStack creates a stack from pull requests ordered from the
// base upward.
// See https://docs.github.com/en/rest/pulls/stacks#create-a-pull-request-stack.
func (c *Gateway) CreatePullRequestStack(
	ctx context.Context,
	input *CreatePullRequestStackInput,
) error {
	if err := validateStackPullRequestCount(len(input.PullRequests), 2); err != nil {
		return err
	}

	req := struct {
		PullRequests []int `json:"pull_requests"`
	}{PullRequests: input.PullRequests}
	if err := c.postREST(
		ctx,
		[]string{"repos", input.Owner, input.Repo, "stacks"},
		&req,
		nil,
	); err != nil {
		return fmt.Errorf("create pull request stack: %w", err)
	}
	return nil
}

// AddPullRequestsToStackInput identifies pull requests to add to an existing
// stack.
type AddPullRequestsToStackInput struct {
	// Owner is the login that owns the repository.
	Owner string // required

	// Repo is the repository name.
	Repo string // required

	// StackNumber identifies the stack within the repository.
	StackNumber int // required

	// PullRequests lists pull request numbers from the current top upward.
	// GitHub accepts between 1 and 100 members.
	PullRequests []int // required
}

// AddPullRequestsToStack adds pull requests above an existing stack.
// See https://docs.github.com/en/rest/pulls/stacks#add-pull-requests-to-a-pull-request-stack.
func (c *Gateway) AddPullRequestsToStack(
	ctx context.Context,
	input *AddPullRequestsToStackInput,
) error {
	if err := validateStackPullRequestCount(len(input.PullRequests), 1); err != nil {
		return err
	}

	req := struct {
		PullRequests []int `json:"pull_requests"`
	}{PullRequests: input.PullRequests}
	if err := c.postREST(
		ctx,
		[]string{
			"repos",
			input.Owner,
			input.Repo,
			"stacks",
			strconv.Itoa(input.StackNumber),
			"add",
		},
		&req,
		nil,
	); err != nil {
		return fmt.Errorf("add pull requests to stack: %w", err)
	}
	return nil
}

func validateStackPullRequestCount(count, minimum int) error {
	if count < minimum || count > maxStackPullRequests {
		return fmt.Errorf(
			"pull request count must be between %d and %d: %d",
			minimum,
			maxStackPullRequests,
			count,
		)
	}
	return nil
}
