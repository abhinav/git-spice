package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// MergeChange merges an open pull request into its base branch.
func (r *Repository) MergeChange(
	ctx context.Context, fid forge.ChangeID,
	opts forge.MergeChangeOptions,
) error {
	id := mustPR(fid)

	gqlID, err := r.graphQLID(ctx, id)
	if err != nil {
		return fmt.Errorf("resolve PR ID: %w", err)
	}

	return r.mergePullRequest(ctx, id, gqlID, opts)
}

func (r *Repository) mergePullRequest(
	ctx context.Context, id *PR, gqlID githubv4.ID,
	opts forge.MergeChangeOptions,
) error {
	var m struct {
		MergePullRequest struct {
			PullRequest struct {
				ID githubv4.ID `graphql:"id"`
			} `graphql:"pullRequest"`
		} `graphql:"mergePullRequest(input: $input)"`
	}

	input := githubv4.MergePullRequestInput{
		PullRequestID: gqlID,
	}
	if opts.HeadHash != "" {
		oid := githubv4.GitObjectID(opts.HeadHash)
		input.ExpectedHeadOid = &oid
	}
	switch opts.Method {
	case forge.MergeMethodDefault:
	case forge.MergeMethodMerge:
		input.MergeMethod = new(githubv4.PullRequestMergeMethodMerge)
	case forge.MergeMethodSquash:
		input.MergeMethod = new(githubv4.PullRequestMergeMethodSquash)
	case forge.MergeMethodRebase:
		input.MergeMethod = new(githubv4.PullRequestMergeMethodRebase)
	default:
		r.log.Warn(
			"Unsupported merge method; using forge default",
			"method", opts.Method,
		)
	}
	if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}

	r.log.Debug("Merged pull request", "pr", id.Number)
	return nil
}
