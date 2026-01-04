package github

import (
	"context"
	"errors"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/graphqlutil"
)

// SubmitChange creates a new change in a repository.
func (r *Repository) SubmitChange(ctx context.Context, req forge.SubmitChangeRequest) (forge.SubmitChangeResult, error) {
	var m struct {
		CreatePullRequest struct {
			PullRequest struct {
				ID     githubv4.ID  `graphql:"id"`
				Number githubv4.Int `graphql:"number"`
				URL    githubv4.URI `graphql:"url"`
			} `graphql:"pullRequest"`
		} `graphql:"createPullRequest(input: $input)"`
	}

	input := githubv4.CreatePullRequestInput{
		RepositoryID: r.repoID,
		Title:        githubv4.String(req.Subject),
		BaseRefName:  githubv4.String(req.Base),
		HeadRefName:  githubv4.String(req.Head),
	}
	if req.Body != "" {
		input.Body = (*githubv4.String)(&req.Body)
	}
	if req.Draft {
		input.Draft = githubv4.NewBoolean(true)
	}

	if err := r.gh4.Mutate(ctx, &m, input, nil); err != nil {
		// If the base branch has not been pushed yet,
		// the error is:
		//   {
		//      "type": "UNPROCESSABLE",
		//      "path": "createPullRequest",
		//      "message": "..., No commits between $base and $head, ..."
		//   }
		// String matching is not the best way to handle this,
		// so if the error is unprocessable,
		// we'll check if the repository has the base branch.
		if errors.Is(err, graphqlutil.ErrUnprocessable) {
			if exists, existsErr := r.RefExists(ctx, "refs/heads/"+req.Base); existsErr == nil && !exists {
				return forge.SubmitChangeResult{}, errors.Join(forge.ErrUnsubmittedBase, err)
			}
		}

		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	r.log.Debug("Created pull request",
		"pr", m.CreatePullRequest.PullRequest.Number,
		"url", m.CreatePullRequest.PullRequest.URL.String())

	// TODO: combine the following into one mutation.

	err := r.addLabelsToPullRequest(ctx, req.Labels, m.CreatePullRequest.PullRequest.ID)
	if err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add labels to PR: %w", err)
	}

	if err := r.addReviewersToPullRequest(ctx, req.Reviewers, int(m.CreatePullRequest.PullRequest.Number)); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add reviewers to PR: %w", err)
	}

	if err := r.addAssigneesToPullRequest(ctx, req.Assignees, m.CreatePullRequest.PullRequest.ID); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("add assignees to PR: %w", err)
	}

	return forge.SubmitChangeResult{
		ID: &PR{
			Number: int(m.CreatePullRequest.PullRequest.Number),
			GQLID:  m.CreatePullRequest.PullRequest.ID,
		},
		URL: m.CreatePullRequest.PullRequest.URL.String(),
	}, nil
}
