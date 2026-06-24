package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"

	"go.abhg.dev/gs/internal/forge"
)

// GitHub StatusCheckRollup.state values, as plain strings (the batch
// checks query below reads the rollup state as an untyped string).
const (
	statusStateExpected = "EXPECTED"
	statusStatePending  = "PENDING"
	statusStateError    = "ERROR"
	statusStateFailure  = "FAILURE"
	statusStateSuccess  = "SUCCESS"
)

// rollupFromStatusCheckRollupState collapses GitHub's
// StatusCheckRollup.state into the forge rollup taxonomy.
//
// An empty input means no rollup exists for the commit, i.e. no
// checks have been reported. Unknown states are treated as failure
// to preserve a fail-safe default; the per-run detail retains the
// native vocabulary.
func rollupFromStatusCheckRollupState(state string) forge.ChecksRollupState {
	switch state {
	case "":
		return forge.ChecksRollupNone
	case statusStateSuccess:
		return forge.ChecksRollupPassed
	case statusStatePending, statusStateExpected:
		return forge.ChecksRollupPending
	case statusStateError, statusStateFailure:
		return forge.ChecksRollupFailed
	default:
		return forge.ChecksRollupFailed
	}
}

// nativeCheckRunState returns the forge-native lowercase string for
// a CheckRun: its conclusion when COMPLETED, otherwise its status.
//
// A COMPLETED run with no conclusion (rare in practice) falls back
// to "completed" so the caller still surfaces a non-empty state.
func nativeCheckRunState(status, conclusion string) string {
	if status == "COMPLETED" {
		if conclusion != "" {
			return strings.ToLower(conclusion)
		}
		return "completed"
	}
	return strings.ToLower(status)
}

// nativeStatusContextState returns the forge-native lowercase string
// for a StatusContext.
func nativeStatusContextState(state string) string {
	return strings.ToLower(state)
}

// checksContextLimit caps how many CheckRun/StatusContext entries we
// fetch per change in a single GraphQL query. GitHub's
// StatusCheckRollup.contexts is a connection; this avoids pagination
// for the common case of single-digit checks per PR.
const checksContextLimit = 50

// changeChecksGraphQL is the GraphQL query shape we issue per PR to
// fetch both the rollup and per-run detail.
//
// The PR's permalink + "/checks" gives the forge checks summary page
// — there is no first-class field for that on PullRequest.
type changeChecksGraphQL struct {
	Node struct {
		PullRequest struct {
			Permalink string
			Commits   struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							State    string
							Contexts struct {
								Nodes []struct {
									Typename      string            `graphql:"__typename"`
									CheckRun      checkRunNode      `graphql:"... on CheckRun"`
									StatusContext statusContextNode `graphql:"... on StatusContext"`
								}
							} `graphql:"contexts(first: $contextLimit)"`
						}
					}
				}
			} `graphql:"commits(last: 1)"`
		} `graphql:"... on PullRequest"`
	} `graphql:"node(id: $id)"`
}

type checkRunNode struct {
	Name       string
	Status     string
	Conclusion string
	DetailsURL string `graphql:"detailsUrl"`
}

type statusContextNode struct {
	Context   string
	State     string
	TargetURL string `graphql:"targetUrl"`
}

// ChecksByChange reports per-change rolled-up and per-run check state
// for each of the given PRs. One GraphQL query per PR; GitHub's
// batched-by-aliases shape is awkward and the per-PR queries are cheap
// relative to the rest of `gs ll` for the common case of single-digit
// stack depth.
func (r *Repository) ChecksByChange(
	ctx context.Context, ids []forge.ChangeID,
) ([]*forge.ChecksReport, error) {
	out := make([]*forge.ChecksReport, len(ids))
	for i, id := range ids {
		checks, err := r.changeChecks(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("change %v: %w", id, err)
		}
		out[i] = checks
	}
	return out, nil
}

func (r *Repository) changeChecks(
	ctx context.Context, id forge.ChangeID,
) (*forge.ChecksReport, error) {
	pr := mustPR(id)
	gqlID, err := r.graphQLID(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("resolve PR ID: %w", err)
	}

	var q changeChecksGraphQL
	if err := r.client.Query(ctx, &q, map[string]any{
		"id":           gqlID,
		"contextLimit": githubv4.Int(checksContextLimit),
	}); err != nil {
		return nil, fmt.Errorf("query checks: %w", err)
	}

	commits := q.Node.PullRequest.Commits.Nodes
	if len(commits) == 0 {
		return &forge.ChecksReport{Rollup: forge.ChecksRollupNone}, nil
	}

	checksURL := ""
	if pl := q.Node.PullRequest.Permalink; pl != "" {
		checksURL = pl + "/checks"
	}

	rollupNode := commits[0].Commit.StatusCheckRollup
	if rollupNode == nil {
		return &forge.ChecksReport{
			Rollup: forge.ChecksRollupNone,
			URL:    checksURL,
		}, nil
	}

	runs := make([]forge.CheckRun, 0, len(rollupNode.Contexts.Nodes))
	for _, node := range rollupNode.Contexts.Nodes {
		switch node.Typename {
		case "CheckRun":
			runs = append(runs, forge.CheckRun{
				Name:  node.CheckRun.Name,
				State: nativeCheckRunState(node.CheckRun.Status, node.CheckRun.Conclusion),
				URL:   node.CheckRun.DetailsURL,
			})
		case "StatusContext":
			runs = append(runs, forge.CheckRun{
				Name:  node.StatusContext.Context,
				State: nativeStatusContextState(node.StatusContext.State),
				URL:   node.StatusContext.TargetURL,
			})
		}
	}

	return &forge.ChecksReport{
		Rollup: rollupFromStatusCheckRollupState(rollupNode.State),
		Runs:   runs,
		URL:    checksURL,
	}, nil
}
