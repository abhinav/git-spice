package github

import (
	"context"
	"fmt"
	"time"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
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
	ctx context.Context, gqlID github.ID,
) ([]forge.ChangeCheck, error) {
	var contexts []github.StatusCheck
	for check, err := range r.gateway.StatusChecks(ctx, gqlID, nil) {
		if err != nil {
			return nil, fmt.Errorf("query status checks: %w", err)
		}
		contexts = append(contexts, check)
	}

	return checksFromGatewayContexts(contexts), nil
}

func checksFromGatewayContexts(
	contexts []github.StatusCheck,
) []forge.ChangeCheck {
	// checkRollupItem is one de-duplicated status or check run.
	//
	// The newest item for a lane wins so stale failed runs do not keep
	// appearing after a newer rerun has passed.
	type checkRollupItem struct {
		Key   checkRollupKey    // de-dupe lane
		Check forge.ChangeCheck // forge-neutral check state
		At    time.Time         // timestamp used to pick the newest item
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

		if item.At.After(existing.At) {
			checks[item.Key] = item
		}
	}

	// GitHub status rollups can include repeated check runs and status
	// contexts for a single logical check lane.
	// Keep only the newest item for each lane on the client side.
	for _, context := range contexts {
		switch context := context.(type) {
		case *github.StatusContext:
			addItem(checkRollupItem{
				Key: checkRollupKey{
					Kind: "status",
					Name: context.Context,
				},
				Check: forge.ChangeCheck{
					Name: context.Context,
					State: changeCheckStateFromStatusState(
						context.State,
					),
				},
				At: context.CreatedAt,
			})
		case *github.CheckRun:
			at := context.StartedAt
			if context.CompletedAt != nil {
				at = *context.CompletedAt
			}
			addItem(checkRollupItem{
				Key: checkRollupKey{
					Kind:     "check_run",
					Name:     context.Name,
					Workflow: context.CheckSuite.WorkflowRun.Workflow.Name,
					Event:    context.CheckSuite.WorkflowRun.Event,
				},
				Check: forge.ChangeCheck{
					Name:  context.Name,
					State: changeCheckStateFromCheckRun(context),
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
	state github.StatusState,
) forge.ChangeCheckState {
	switch state {
	case github.StatusStateSuccess:
		return forge.ChangeCheckPassed
	case github.StatusStatePending, github.StatusStateExpected:
		return forge.ChangeCheckPending
	case github.StatusStateError, github.StatusStateFailure:
		return forge.ChangeCheckFailed
	default:
		return forge.ChangeCheckFailed
	}
}

func changeCheckStateFromCheckRun(
	checkRun *github.CheckRun,
) forge.ChangeCheckState {
	switch {
	case checkRun.Status != github.CheckStatusStateCompleted:
		return forge.ChangeCheckPending
	case checkRun.Conclusion == nil:
		return forge.ChangeCheckFailed
	}

	switch *checkRun.Conclusion {
	case github.CheckConclusionStateSuccess, github.CheckConclusionStateNeutral,
		github.CheckConclusionStateSkipped:
		return forge.ChangeCheckPassed
	case github.CheckConclusionStateActionRequired,
		github.CheckConclusionStateCancelled,
		github.CheckConclusionStateFailure,
		github.CheckConclusionStateStale,
		github.CheckConclusionStateStartupFailure,
		github.CheckConclusionStateTimedOut:
		return forge.ChangeCheckFailed
	default:
		return forge.ChangeCheckFailed
	}
}
