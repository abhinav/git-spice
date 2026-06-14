package github

import (
	"context"
	"fmt"

	"github.com/shurcooL/githubv4"
	"go.abhg.dev/gs/internal/forge"
)

// ChangeChecks reports CI/checks for the given pull request.
func (r *Repository) ChangeChecks(
	ctx context.Context, fid forge.ChangeID,
) ([]forge.ChangeCheck, error) {
	pr := mustPR(fid)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("resolve PR ID: %w", err)
	}

	return r.queryChecksRollup(ctx, gqlID)
}

func (r *Repository) queryChecksRollup(
	ctx context.Context, gqlID githubv4.ID,
) ([]forge.ChangeCheck, error) {
	var contexts []statusCheckRollupContext
	var after *githubv4.String
	for {
		var q struct {
			Node struct {
				PullRequest struct {
					Commits struct {
						Nodes []struct {
							Commit struct {
								StatusCheckRollup *struct {
									Contexts struct {
										Nodes    []statusCheckRollupContext `graphql:"nodes"`
										PageInfo struct {
											EndCursor   githubv4.String `graphql:"endCursor"`
											HasNextPage bool            `graphql:"hasNextPage"`
										} `graphql:"pageInfo"`
									} `graphql:"contexts(first: 100, after: $after)"`
								} `graphql:"statusCheckRollup"`
							} `graphql:"commit"`
						} `graphql:"nodes"`
					} `graphql:"commits(last: 1)"`
				} `graphql:"... on PullRequest"`
			} `graphql:"node(id: $id)"`
		}

		err := r.client.Query(ctx, &q, map[string]any{
			"id":    gqlID,
			"after": after,
		})
		if err != nil {
			return nil, fmt.Errorf("query status checks: %w", err)
		}

		commits := q.Node.PullRequest.Commits.Nodes
		if len(commits) == 0 {
			return nil, nil
		}

		rollup := commits[0].Commit.StatusCheckRollup
		if rollup == nil {
			return nil, nil
		}

		contexts = append(contexts, rollup.Contexts.Nodes...)
		if !rollup.Contexts.PageInfo.HasNextPage {
			break
		}
		after = &rollup.Contexts.PageInfo.EndCursor
	}

	return checksFromRollupContexts(contexts), nil
}

// statusCheckRollupContext is one GitHub status-check rollup context.
//
// GitHub represents classic commit statuses and check runs as different node
// types under the same rollup connection.
// The older Commit.status field covers only classic commit statuses,
// so using it would drop GitHub Actions and Checks API results.
type statusCheckRollupContext struct {
	StatusContext statusContextRollupContext `graphql:"... on StatusContext"`

	CheckRun checkRunRollupContext `graphql:"... on CheckRun"`
}

// statusContextRollupContext is a classic GitHub commit status.
type statusContextRollupContext struct {
	Context   string               `graphql:"context"`
	State     githubv4.StatusState `graphql:"state"`
	CreatedAt githubv4.DateTime    `graphql:"createdAt"`
}

// checkRunRollupContext is a GitHub check run in a status-check rollup.
type checkRunRollupContext struct {
	Name       string `graphql:"name"`
	CheckSuite struct {
		WorkflowRun struct {
			Event    string `graphql:"event"`
			Workflow struct {
				Name string `graphql:"name"`
			} `graphql:"workflow"`
		} `graphql:"workflowRun"`
	} `graphql:"checkSuite"`
	Status      githubv4.CheckStatusState      `graphql:"status"`
	Conclusion  *githubv4.CheckConclusionState `graphql:"conclusion"`
	StartedAt   githubv4.DateTime              `graphql:"startedAt"`
	CompletedAt *githubv4.DateTime             `graphql:"completedAt"`
}

func checksFromRollupContexts(
	contexts []statusCheckRollupContext,
) []forge.ChangeCheck {
	// checkRollupItem is one de-duplicated status or check run.
	//
	// The newest item for a lane wins so stale failed runs do not keep
	// appearing after a newer rerun has passed.
	type checkRollupItem struct {
		Key   checkRollupKey    // de-dupe lane
		Check forge.ChangeCheck // forge-neutral check state
		At    githubv4.DateTime // timestamp used to pick the newest item
	}

	var order []checkRollupKey
	checks := make(map[checkRollupKey]checkRollupItem, len(contexts))

	addItem := func(item checkRollupItem) {
		existing, ok := checks[item.Key]
		if !ok {
			order = append(order, item.Key)
			checks[item.Key] = item
			return
		}

		if item.At.After(existing.At.Time) {
			checks[item.Key] = item
		}
	}

	// GitHub status rollups can include repeated check runs and status
	// contexts for a single logical check lane.
	// Keep only the newest item for each lane on the client side.
	for _, context := range contexts {
		switch {
		case context.StatusContext.Context != "":
			addItem(checkRollupItem{
				Key: checkRollupKey{
					Kind: "status",
					Name: context.StatusContext.Context,
				},
				Check: forge.ChangeCheck{
					Name: context.StatusContext.Context,
					State: changeCheckStateFromStatusState(
						context.StatusContext.State,
					),
				},
				At: context.StatusContext.CreatedAt,
			})
		case context.CheckRun.Name != "":
			at := context.CheckRun.StartedAt
			if context.CheckRun.CompletedAt != nil {
				at = *context.CheckRun.CompletedAt
			}
			addItem(checkRollupItem{
				Key: checkRollupKey{
					Kind:     "check_run",
					Name:     context.CheckRun.Name,
					Workflow: context.CheckRun.CheckSuite.WorkflowRun.Workflow.Name,
					Event:    context.CheckRun.CheckSuite.WorkflowRun.Event,
				},
				Check: forge.ChangeCheck{
					Name:  context.CheckRun.Name,
					State: changeCheckStateFromCheckRun(context.CheckRun),
				},
				At: at,
			})
		}
	}

	result := make([]forge.ChangeCheck, 0, len(order))
	for _, key := range order {
		result = append(result, checks[key].Check)
	}
	return result
}

// checkRollupKey identifies one visible GitHub check lane.
//
// GitHub status-check rollups can include multiple objects with the same
// displayed name when a check is rerun.
// Object IDs identify individual runs,
// so they are not useful for collapsing a visible check lane.
// Keep classic statuses and check runs separate because GitHub models them as
// different signal kinds even when they share a display name.
// Match GitHub CLI's check-run key of name, workflow, and event:
// https://github.com/cli/cli/blob/0274077b56a5ef8e575358721149cd02888b2a5f/pkg/cmd/pr/checks/aggregate.go#L95-L119
type checkRollupKey struct {
	Kind     string // GraphQL union member kind
	Name     string // StatusContext context or CheckRun name
	Workflow string // CheckRun workflow name
	Event    string // CheckRun workflow event
}

func changeCheckStateFromStatusState(
	state githubv4.StatusState,
) forge.ChangeCheckState {
	switch state {
	case githubv4.StatusStateSuccess:
		return forge.ChangeCheckPassed
	case githubv4.StatusStatePending, githubv4.StatusStateExpected:
		return forge.ChangeCheckPending
	case githubv4.StatusStateError, githubv4.StatusStateFailure:
		return forge.ChangeCheckFailed
	default:
		return forge.ChangeCheckFailed
	}
}

func changeCheckStateFromCheckRun(
	checkRun checkRunRollupContext,
) forge.ChangeCheckState {
	switch {
	case checkRun.Status != githubv4.CheckStatusStateCompleted:
		return forge.ChangeCheckPending
	case checkRun.Conclusion == nil:
		return forge.ChangeCheckFailed
	}

	switch *checkRun.Conclusion {
	case githubv4.CheckConclusionStateSuccess,
		githubv4.CheckConclusionStateNeutral,
		githubv4.CheckConclusionStateSkipped:
		return forge.ChangeCheckPassed
	case githubv4.CheckConclusionStateActionRequired,
		githubv4.CheckConclusionStateCancelled,
		githubv4.CheckConclusionStateFailure,
		githubv4.CheckConclusionStateStale,
		githubv4.CheckConclusionStateStartupFailure,
		githubv4.CheckConclusionStateTimedOut:
		return forge.ChangeCheckFailed
	default:
		return forge.ChangeCheckFailed
	}
}
