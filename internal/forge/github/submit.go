package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
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

	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return forge.SubmitChangeResult{}, fmt.Errorf("create pull request: %w", err)
	}

	return forge.SubmitChangeResult{
		ID:  forge.ChangeID(m.CreatePullRequest.PullRequest.Number),
		URL: m.CreatePullRequest.PullRequest.URL.String(),
	}, nil
}
