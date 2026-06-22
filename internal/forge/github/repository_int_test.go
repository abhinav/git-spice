package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

// NewRepository re-exports the private NewRepository function
// for testing.
var NewRepository = newRepository

func CloseChange(ctx context.Context, repo *Repository, id *PR) error {
	var m struct {
		UpdatePullRequest struct {
			PullRequest struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"pullRequest"`
		} `graphql:"updatePullRequest(input: $input)"`
	}
	state := githubv4.PullRequestUpdateStateClosed
	input := githubv4.UpdatePullRequestInput{
		PullRequestID: id.GQLID,
		State:         &state,
	}

	if err := repo.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("close pull request: %w", err)
	}

	repo.log.Debug("Closed pull request", "pr", id.Number)
	return nil
}
