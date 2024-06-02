package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
)

// EditChangeOptions specifies options for an operation to edit
// an existing change.
type EditChangeOptions struct {
	// Base specifies the name of the base branch.
	//
	// If unset, the base branch is not changed.
	Base string

	// Draft specifies whether the change should be marked as a draft.
	// If unset, the draft status is not changed.
	Draft *bool
}

// EditChange edits an existing change in a repository.
func (f *Forge) EditChange(ctx context.Context, id ChangeID, opts EditChangeOptions) error {
	// We don't know the GraphQL ID for the PR, so find it.
	var graphQLID githubv4.ID
	{
		// TODO: Record both, PR number and GraphQL ID, in the store.
		var q struct {
			Repository struct {
				PullRequest struct {
					ID githubv4.ID `graphql:"id"`
				} `graphql:"pullRequest(number: $number)"`
			} `graphql:"repository(owner: $owner, name: $repo)"`
		}
		if err := f.client.Query(ctx, &q, map[string]any{
			"owner":  githubv4.String(f.owner),
			"repo":   githubv4.String(f.repo),
			"number": githubv4.Int(id),
		}); err != nil {
			return fmt.Errorf("get pull request ID: %w", err)
		}
		graphQLID = q.Repository.PullRequest.ID
	}

	if opts.Base != "" {
		var m struct {
			UpdatePullRequest struct {
				// We don't need any information back,
				// so just anything non-empty will suffice as a query.
				ClientMutationID string `graphql:"clientMutationId"`
			} `graphql:"updatePullRequest(input: $input)"`
		}

		input := githubv4.UpdatePullRequestInput{
			PullRequestID: graphQLID,
			BaseRefName:   (*githubv4.String)(&opts.Base),
		}

		if err := f.client.Mutate(ctx, &m, input, nil); err != nil {
			return fmt.Errorf("edit pull request: %w", err)
		}
	}

	// Draft status is a separate API call for some reason.
	if opts.Draft != nil {
		// And for some reason, it's a different mutation based on
		// whether it's true or false.
		var m, input any
		if *opts.Draft {
			m = struct {
				ConvertPullRequestToDraft struct {
					PullRequest struct {
						ID githubv4.ID `graphql:"id"`
					} `graphql:"pullRequest"`
				} `graphql:"convertPullRequestToDraft(input: $input)"`
			}{}

			input = githubv4.ConvertPullRequestToDraftInput{
				PullRequestID: graphQLID,
			}
		} else {
			m = struct {
				MarkPullRequestReadyForReview struct {
					PullRequest struct {
						ID githubv4.ID `graphql:"id"`
					} `graphql:"pullRequest"`
				} `graphql:"markPullRequestReadyForReview(input: $input)"`
			}{}

			input = githubv4.MarkPullRequestReadyForReviewInput{
				PullRequestID: graphQLID,
			}
		}

		if err := f.client.Mutate(ctx, &m, input, nil); err != nil {
			return fmt.Errorf("update draft status: %w", err)
		}
	}

	return nil
}
