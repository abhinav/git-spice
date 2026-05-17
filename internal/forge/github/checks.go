package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangeChecksStatus reports the aggregate CI/checks state
// for the given pull request.
func (r *Repository) ChangeChecksStatus(
	ctx context.Context, fid forge.ChangeID,
) (forge.ChecksState, error) {
	pr := mustPR(fid)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return 0, fmt.Errorf("resolve PR ID: %w", err)
	}

	return r.queryChecksRollup(ctx, gqlID)
}

func (r *Repository) queryChecksRollup(
	ctx context.Context, gqlID githubv4.ID,
) (forge.ChecksState, error) {
	var q struct {
		Node struct {
			PullRequest struct {
				Commits struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								State string
							}
						}
					}
				} `graphql:"commits(last: 1)"`
			} `graphql:"... on PullRequest"`
		} `graphql:"node(id: $id)"`
	}

	err := r.client.Query(ctx, &q, map[string]any{
		"id": gqlID,
	})
	if err != nil {
		return 0, fmt.Errorf("query status checks: %w", err)
	}

	return parseRollupState(q.Node.PullRequest.Commits.Nodes), nil
}

func parseRollupState(
	commits []struct {
		Commit struct {
			StatusCheckRollup *struct {
				State string
			}
		}
	},
) forge.ChecksState {
	if len(commits) == 0 {
		return forge.ChecksPassed
	}

	rollup := commits[0].Commit.StatusCheckRollup
	if rollup == nil {
		return forge.ChecksPassed // no checks configured
	}

	switch rollup.State {
	case "SUCCESS", "EXPECTED":
		return forge.ChecksPassed
	case "PENDING":
		return forge.ChecksPending
	default:
		return forge.ChecksFailed
	}
}
