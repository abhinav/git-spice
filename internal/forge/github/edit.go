package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/cmputil"
	"go.abhg.dev/gs/internal/forge"
)

// EditChange edits an existing change in a repository.
func (r *Repository) EditChange(ctx context.Context, fid forge.ChangeID, opts forge.EditChangeOptions) error {
	if cmputil.Zero(opts) {
		return nil // nothing to do
	}

	// We don't know the GraphQL ID for the PR, so find it.
	graphQLID, err := r.graphQLID(ctx, mustPR(fid))
	if err != nil {
		return fmt.Errorf("get pull request ID: %w", err)
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

		if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
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

		if err := r.client.Mutate(ctx, &m, input, nil); err != nil {
			return fmt.Errorf("update draft status: %w", err)
		}
	}

	return nil
}
