package github

import (
	"context"
	"fmt"
	"strings"

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

// rollupFromStatusCheckRollupState collapses GitHub's
// StatusCheckRollup.state into the upstream rollup taxonomy.
//
// An empty input means no rollup exists for the commit, i.e. no
// checks have been reported. Unknown states are treated as failure
// to preserve a fail-safe default; the per-run detail retains the
// native vocabulary.
func rollupFromStatusCheckRollupState(state string) forge.ChecksState {
	switch state {
	case "":
		return forge.ChecksNone
	case statusStateSuccess:
		return forge.ChecksPassed
	case statusStatePending, statusStateExpected:
		return forge.ChecksPending
	case statusStateError, statusStateFailure:
		return forge.ChecksFailed
	default:
		return forge.ChecksFailed
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
		// Pre-richer-API behavior is to treat unknown as passed so
		// merge-readiness doesn't false-fail. Preserved here.
		return forge.ChecksPassed, nil
	}

	rollup := commits[0].Commit.StatusCheckRollup
	if rollup == nil {
		return forge.ChecksPassed, nil
	}

	// ChangeChecksState (used by the merge handler) maps "no rollup"
	// to passed. ChecksByChange uses the richer rollupFrom mapping
	// (no rollup → none) instead; both go through the same enum.
	s := rollupFromStatusCheckRollupState(rollup.State)
	if s == forge.ChecksNone {
		return forge.ChecksPassed, nil
	}
	return s, nil
}

// checksContextLimit caps how many CheckRun/StatusContext entries
// we fetch per change in a single GraphQL query. GitHub's
// StatusCheckRollup.contexts is a connection; this avoids pagination
// for the common case of single-digit checks per PR.
const checksContextLimit = 50

// changeChecksGraphQL is the GraphQL query shape we issue per PR
// to fetch both the rollup and per-run detail.
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
// batched-by-aliases shape is awkward and the per-PR queries are
// cheap relative to the rest of `gs ll` for the common case of
// single-digit stack depth.
func (r *Repository) ChecksByChange(
	ctx context.Context, ids []forge.ChangeID,
) ([]*forge.ChangeChecks, error) {
	out := make([]*forge.ChangeChecks, len(ids))
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
) (*forge.ChangeChecks, error) {
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
		return &forge.ChangeChecks{Rollup: forge.ChecksNone}, nil
	}

	checksURL := ""
	if pl := q.Node.PullRequest.Permalink; pl != "" {
		checksURL = pl + "/checks"
	}

	rollupNode := commits[0].Commit.StatusCheckRollup
	if rollupNode == nil {
		return &forge.ChangeChecks{
			Rollup: forge.ChecksNone,
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

	return &forge.ChangeChecks{
		Rollup: rollupFromStatusCheckRollupState(rollupNode.State),
		Runs:   runs,
		URL:    checksURL,
	}, nil
}
