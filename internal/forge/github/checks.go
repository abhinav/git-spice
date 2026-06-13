package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// GitHub StatusState values.
//
// https://docs.github.com/en/graphql/reference/enums#statusstate
const (
	statusStateError    = "ERROR"
	statusStateExpected = "EXPECTED"
	statusStateFailure  = "FAILURE"
	statusStatePending  = "PENDING"
	statusStateSuccess  = "SUCCESS"
)

// ChangeChecksState reports the aggregate CI/checks state
// for the given pull request.
func (r *Repository) ChangeChecksState(
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

	commits := q.Node.PullRequest.Commits.Nodes
	if len(commits) == 0 {
		return forge.ChecksPassed, nil
	}

	rollup := commits[0].Commit.StatusCheckRollup
	if rollup == nil {
		return forge.ChecksPassed, nil // no checks configured
	}

	switch rollup.State {
	case statusStateSuccess:
		return forge.ChecksPassed, nil
	case statusStatePending, statusStateExpected:
		return forge.ChecksPending, nil
	case statusStateError, statusStateFailure:
		return forge.ChecksFailed, nil
	default:
		return forge.ChecksFailed, nil
	}
}

// ChecksByChange reports per-change rolled-up and per-run check state
// for each of the given PRs.
//
// TODO: real implementation lands on a follow-up branch.
// This stub returns one nil per id to satisfy the [forge.Repository]
// interface while the schema branch lands standalone.
func (r *Repository) ChecksByChange(
	_ context.Context, ids []forge.ChangeID,
) ([]*forge.ChangeChecks, error) {
	return make([]*forge.ChangeChecks, len(ids)), nil
}
