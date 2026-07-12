package github

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// StatusCheck is one gateway-normalized member of GitHub's status-check union.
// Its dynamic type is either [StatusContext] or [CheckRun].
// Future union members that the gateway does not recognize are omitted from
// [StatusChecksPage.Contexts] so known checks remain available.
type StatusCheck interface {
	isStatusCheck()
}

// StatusContext is a classic commit status in a status rollup.
type StatusContext struct {
	// Context is the status name supplied by the reporting system.
	Context string

	// State is the status's current state.
	State StatusState

	// CreatedAt is the time GitHub created the status.
	CreatedAt time.Time
}

func (*StatusContext) isStatusCheck() {}

// CheckRun is a GitHub Actions or external check run in a status rollup.
type CheckRun struct {
	// Name is the check run's display name.
	Name string

	// CheckSuite identifies the workflow source of the check run.
	CheckSuite struct {
		// WorkflowRun identifies the workflow invocation that produced the check.
		WorkflowRun struct {
			// Event is the event that triggered the workflow run.
			Event string

			// Workflow identifies the workflow definition that produced the run.
			Workflow struct {
				// Name is the workflow's display name.
				Name string
			}
		}
	}

	// Status is the check run's lifecycle state.
	Status CheckStatusState

	// Conclusion is the terminal result, or nil while no result is available.
	Conclusion *CheckConclusionState

	// StartedAt is the time GitHub started the check run.
	StartedAt time.Time

	// CompletedAt is the time GitHub completed the check run, or nil while the
	// run has not completed.
	CompletedAt *time.Time
}

func (*CheckRun) isStatusCheck() {}

// StatusChecksPage is one page of a pull request's status-check rollup.
type StatusChecksPage struct {
	// Contexts contains the known status and check-run members on this page.
	// Future unknown union members are omitted for forward compatibility.
	Contexts []StatusCheck

	// EndCursor is usable as after when HasNextPage is true.
	EndCursor string

	// HasNextPage reports whether another page follows EndCursor.
	HasNextPage bool

	// Present reports whether the pull request has a status-check rollup.
	Present bool
}

// StatusChecks loads up to 100 status-check rollup contexts.
// A nil after selects the first page; a non-nil after continues after that
// cursor. Present is false when the pull request has no status-check rollup.
func (c *Gateway) StatusChecks(ctx context.Context, id ID, after *string) (*StatusChecksPage, error) {
	query := compactGraphQL(`
		query($after:String$id:ID!){
			node(id: $id){
				... on PullRequest{
					commits(last: 1){
						nodes{commit{statusCheckRollup{
							contexts(first: 100, after: $after){
								nodes{
									... on StatusContext{context,state,createdAt},
									... on CheckRun{
										name,
										checkSuite{workflowRun{event,workflow{name}}},
										status,conclusion,startedAt,completedAt
									}
								},
								pageInfo{endCursor,hasNextPage}
							}
						}}}
					}
				}
			}
		}
	`)
	vars := struct {
		After *string `json:"after"`
		ID    ID      `json:"id"`
	}{after, id}
	var result statusChecksResult
	if err := c.execute(ctx, query, vars, &result); err != nil {
		return nil, fmt.Errorf("query status checks: %w", err)
	}

	contexts, ok := result.statusCheckContexts()
	if !ok {
		return &StatusChecksPage{}, nil
	}
	normalized, err := normalizeStatusChecks(contexts.Nodes)
	if err != nil {
		return nil, err
	}
	return &StatusChecksPage{
		Contexts:    normalized,
		EndCursor:   contexts.PageInfo.EndCursor,
		HasNextPage: contexts.PageInfo.HasNextPage,
		Present:     true,
	}, nil
}

type statusChecksResult struct {
	Node struct {
		Commits struct {
			Nodes []struct {
				Commit struct {
					StatusCheckRollup *struct {
						Contexts statusCheckConnection `json:"contexts"`
					} `json:"statusCheckRollup"`
				} `json:"commit"`
			} `json:"nodes"`
		} `json:"commits"`
	} `json:"node"`
}

type statusCheckConnection struct {
	Nodes []*statusCheckWire `json:"nodes"`

	PageInfo struct {
		EndCursor   string `json:"endCursor"`
		HasNextPage bool   `json:"hasNextPage"`
	} `json:"pageInfo"`
}

type statusCheckWire struct {
	Context    *string     `json:"context"`
	State      StatusState `json:"state"`
	CreatedAt  time.Time   `json:"createdAt"`
	Name       *string     `json:"name"`
	CheckSuite struct {
		WorkflowRun struct {
			Event    string `json:"event"`
			Workflow struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
	Status      CheckStatusState      `json:"status"`
	Conclusion  *CheckConclusionState `json:"conclusion"`
	StartedAt   time.Time             `json:"startedAt"`
	CompletedAt *time.Time            `json:"completedAt"`
}

func (r *statusChecksResult) statusCheckContexts() (statusCheckConnection, bool) {
	if len(r.Node.Commits.Nodes) == 0 {
		return statusCheckConnection{}, false
	}
	rollup := r.Node.Commits.Nodes[0].Commit.StatusCheckRollup
	if rollup == nil {
		return statusCheckConnection{}, false
	}
	return rollup.Contexts, true
}

func normalizeStatusChecks(nodes []*statusCheckWire) ([]StatusCheck, error) {
	checks := make([]StatusCheck, 0, len(nodes))
	for _, node := range nodes {
		check, known, err := node.statusCheck()
		if err != nil {
			return nil, err
		}
		if known {
			checks = append(checks, check)
		}
	}
	return checks, nil
}

func (w *statusCheckWire) statusCheck() (StatusCheck, bool, error) {
	switch {
	case w.Context != nil && w.Name == nil:
		return &StatusContext{Context: *w.Context, State: w.State, CreatedAt: w.CreatedAt}, true, nil

	case w.Name != nil && w.Context == nil:
		check := CheckRun{
			Name:        *w.Name,
			Status:      w.Status,
			Conclusion:  w.Conclusion,
			StartedAt:   w.StartedAt,
			CompletedAt: w.CompletedAt,
		}
		check.CheckSuite.WorkflowRun.Event = w.CheckSuite.WorkflowRun.Event
		check.CheckSuite.WorkflowRun.Workflow.Name = w.CheckSuite.WorkflowRun.Workflow.Name
		return &check, true, nil

	case w.Context != nil && w.Name != nil:
		return nil, false, errors.New("status-check context has ambiguous union member")

	default:
		// GitHub returns an empty object for union members not covered by the
		// requested fragments. Keep the known checks usable when GitHub adds a
		// new member to the union.
		return nil, false, nil
	}
}
