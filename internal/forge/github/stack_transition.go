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
// native stack membership. An empty pullRequests slice skips the membership
// write; otherwise stackNumber selects create (zero) or append.
type githubStackPathTransition struct {
	baseUpdates []githubPullRequestBaseUpdate

	stackNumber  int
	pullRequests []int
}

type githubPullRequestBaseUpdate struct {
	pullRequestNumber int
	pullRequestID     github.ID
	baseBranch        string
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
		if pathFailed || len(path.pullRequests) == 0 {
			continue
		}

		if path.stackNumber == 0 {
			if err := repository.gateway.CreatePullRequestStack(
				ctx,
				&github.CreatePullRequestStackInput{
					Owner:        repository.owner,
					Repo:         repository.repo,
					PullRequests: slices.Clone(path.pullRequests),
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
				StackNumber:  path.stackNumber,
				PullRequests: slices.Clone(path.pullRequests),
			},
		); err != nil {
			errs = append(errs, fmt.Errorf(
				"extend GitHub native stack #%d: %w",
				path.stackNumber,
				err,
			))
		}
	}
	return errors.Join(errs...)
}
