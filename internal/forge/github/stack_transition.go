package github

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.abhg.dev/gs/internal/gateway/github"
)

// githubStackTransition describes one independent remote stack update.
// The unstack number, when non-zero, must succeed before any path begins.
// Paths remain independent after that shared prerequisite.
type githubStackTransition struct {
	unstackNumber int
	paths         []githubStackPathTransition
}

// githubStackPathTransition aligns one path's bases before publishing its
// native stack membership. A nil stack update leaves the path unstacked.
type githubStackPathTransition struct {
	baseUpdates []githubPullRequestBaseUpdate
	stackUpdate *githubStackMembershipUpdate
}

type githubPullRequestBaseUpdate struct {
	pullRequestNumber int
	pullRequestID     github.ID
	baseBranch        string
}

// githubStackMembershipUpdate creates a native stack when stackNumber is zero
// and appends to that existing stack otherwise.
type githubStackMembershipUpdate struct {
	stackNumber  int
	pullRequests []int
}

func newGitHubStackTransition(
	candidate githubStackTransitionCandidate,
) githubStackTransition {
	var transition githubStackTransition
	if candidate.kind == githubStackTransitionReplace {
		transition.unstackNumber = candidate.remoteStack.Number
	}

	for _, selected := range candidate.paths {
		start := 0
		if candidate.kind == githubStackTransitionAppend {
			start = len(openStackMemberNumbers(candidate.remoteStack.Members))
		}

		// Retarget bottom-to-top before publishing membership. Once a write in
		// this path fails, later writes would rely on a base relationship that was
		// never established.
		var path githubStackPathTransition
		for _, change := range selected[start:] {
			if change.baseBranch == "" || change.pullRequest.baseBranch == change.baseBranch {
				continue
			}
			path.baseUpdates = append(path.baseUpdates, githubPullRequestBaseUpdate{
				pullRequestNumber: change.number,
				pullRequestID:     change.pullRequest.id,
				baseBranch:        change.baseBranch,
			})
		}

		numbers := stackChangeNumbers(selected)
		switch {
		case len(numbers) < 2:
		case candidate.kind == githubStackTransitionAppend:
			path.stackUpdate = &githubStackMembershipUpdate{
				stackNumber:  candidate.remoteStack.Number,
				pullRequests: slices.Clone(numbers[start:]),
			}
		default:
			path.stackUpdate = &githubStackMembershipUpdate{
				pullRequests: slices.Clone(numbers),
			}
		}
		if len(path.baseUpdates) > 0 || path.stackUpdate != nil {
			transition.paths = append(transition.paths, path)
		}
	}
	return transition
}

func (t githubStackTransition) execute(
	ctx context.Context,
	repository *Repository,
) error {
	// A replacement must dissolve the old provider object completely before
	// any base or membership write may use the desired topology.
	if t.unstackNumber != 0 {
		result, err := repository.gateway.UnstackPullRequestStack(
			ctx,
			&github.UnstackPullRequestStackInput{
				Owner:       repository.owner,
				Repo:        repository.repo,
				StackNumber: t.unstackNumber,
			},
		)
		if err != nil {
			return fmt.Errorf("unstack GitHub native stack #%d: %w", t.unstackNumber, err)
		}
		if len(result.RemainingPullRequests) > 0 {
			return fmt.Errorf(
				"unstack GitHub native stack #%d: GitHub retained pull requests %v",
				t.unstackNumber,
				result.RemainingPullRequests,
			)
		}
	}

	var errs []error
	for _, path := range t.paths {
		pathFailed := false
		for _, update := range path.baseUpdates {
			base := update.baseBranch
			if err := repository.gateway.UpdatePullRequest(ctx, &github.UpdatePullRequestInput{
				PullRequestID: update.pullRequestID,
				BaseRefName:   &base,
			}); err != nil {
				errs = append(errs, fmt.Errorf(
					"update GitHub pull request #%d base: %w",
					update.pullRequestNumber,
					err,
				))
				pathFailed = true
				break
			}
		}
		if pathFailed || path.stackUpdate == nil {
			continue
		}

		stackUpdate := path.stackUpdate
		if stackUpdate.stackNumber == 0 {
			if err := repository.gateway.CreatePullRequestStack(
				ctx,
				&github.CreatePullRequestStackInput{
					Owner:        repository.owner,
					Repo:         repository.repo,
					PullRequests: slices.Clone(stackUpdate.pullRequests),
				},
			); err != nil {
				errs = append(errs, fmt.Errorf("create GitHub native stack: %w", err))
			}
			continue
		}

		if err := repository.gateway.AddPullRequestsToStack(
			ctx,
			&github.AddPullRequestsToStackInput{
				Owner:        repository.owner,
				Repo:         repository.repo,
				StackNumber:  stackUpdate.stackNumber,
				PullRequests: slices.Clone(stackUpdate.pullRequests),
			},
		); err != nil {
			errs = append(errs, fmt.Errorf(
				"extend GitHub native stack #%d: %w",
				stackUpdate.stackNumber,
				err,
			))
		}
	}
	return errors.Join(errs...)
}

func stackChangeNumbers(changes []*githubStackChange) []int {
	numbers := make([]int, len(changes))
	for i, change := range changes {
		numbers[i] = change.number
	}
	return numbers
}
