package github

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/gateway/github"
)

type statusCheckRollupContext struct {
	StatusContext statusContextRollupContext
	CheckRun      checkRunRollupContext
}

type statusContextRollupContext struct {
	Context   string
	State     github.StatusState
	CreatedAt time.Time
}

type checkRunRollupContext struct {
	Name       string
	CheckSuite struct {
		WorkflowRun struct {
			Event    string
			Workflow struct{ Name string }
		}
	}
	Status      github.CheckStatusState
	Conclusion  *github.CheckConclusionState
	StartedAt   time.Time
	CompletedAt *time.Time
}

func checksFromRollupContexts(contexts []statusCheckRollupContext) []forge.ChangeCheck {
	converted := make([]github.StatusCheck, 0, len(contexts))
	for _, context := range contexts {
		if context.StatusContext.Context != "" {
			converted = append(converted, &github.StatusContext{
				Context:   context.StatusContext.Context,
				State:     context.StatusContext.State,
				CreatedAt: context.StatusContext.CreatedAt,
			})
			continue
		}
		checkRun := github.CheckRun{
			Name:        context.CheckRun.Name,
			Status:      context.CheckRun.Status,
			Conclusion:  context.CheckRun.Conclusion,
			StartedAt:   context.CheckRun.StartedAt,
			CompletedAt: context.CheckRun.CompletedAt,
		}
		checkRun.CheckSuite.WorkflowRun.Event = context.CheckRun.CheckSuite.WorkflowRun.Event
		checkRun.CheckSuite.WorkflowRun.Workflow.Name = context.CheckRun.CheckSuite.WorkflowRun.Workflow.Name
		converted = append(converted, &checkRun)
	}
	return checksFromGatewayContexts(converted)
}

func TestChecksFromRollupContexts_statusContexts(t *testing.T) {
	var contexts []statusCheckRollupContext
	contexts = append(contexts,
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context: "git-spice integration",
				State:   github.StatusStateSuccess,
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context: "lint",
				State:   github.StatusStatePending,
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context: "test",
				State:   github.StatusStateFailure,
			},
		},
	)

	assert.Equal(t, []forge.ChangeCheck{
		{Name: "git-spice integration", State: forge.ChangeCheckPassed},
		{Name: "lint", State: forge.ChangeCheckPending},
		{Name: "test", State: forge.ChangeCheckFailed},
	}, checksFromRollupContexts(contexts))
}

func TestChecksFromRollupContexts_checkRuns(t *testing.T) {
	successConclusion := github.CheckConclusionStateSuccess
	failureConclusion := github.CheckConclusionStateFailure

	var contexts []statusCheckRollupContext
	contexts = append(contexts,
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:   "build",
				Status: github.CheckStatusStateInProgress,
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:       "unit",
				Status:     github.CheckStatusStateCompleted,
				Conclusion: &successConclusion,
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:       "integration",
				Status:     github.CheckStatusStateCompleted,
				Conclusion: &failureConclusion,
			},
		},
	)

	assert.Equal(t, []forge.ChangeCheck{
		{Name: "build", State: forge.ChangeCheckPending},
		{Name: "unit", State: forge.ChangeCheckPassed},
		{Name: "integration", State: forge.ChangeCheckFailed},
	}, checksFromRollupContexts(contexts))
}

func TestChecksFromRollupContexts_deduplicatesByGitHubCheckLane(t *testing.T) {
	successConclusion := github.CheckConclusionStateSuccess
	failureConclusion := github.CheckConclusionStateFailure

	var contexts []statusCheckRollupContext
	contexts = append(contexts,
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "unit",
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "unit",
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:05:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "integration",
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:10:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "integration",
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:01:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "test",
				CheckSuite:  checkSuite("push", "linux"),
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "test",
				CheckSuite:  checkSuite("pull_request", "linux"),
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "test",
				CheckSuite:  checkSuite("push", "windows"),
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context:   "deploy",
				State:     github.StatusStatePending,
				CreatedAt: dateTime(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context:   "deploy",
				State:     github.StatusStateSuccess,
				CreatedAt: dateTime(t, "2026-06-19T10:05:00Z"),
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context:   "shared",
				State:     github.StatusStateSuccess,
				CreatedAt: dateTime(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "shared",
				Status:      github.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:05:00Z"),
			},
		},
	)

	assert.Equal(t, []forge.ChangeCheck{
		{Name: "unit", State: forge.ChangeCheckPassed},
		{Name: "integration", State: forge.ChangeCheckPassed},
		{Name: "test", State: forge.ChangeCheckPassed},
		{Name: "test", State: forge.ChangeCheckFailed},
		{Name: "test", State: forge.ChangeCheckPassed},
		{Name: "deploy", State: forge.ChangeCheckPassed},
		{Name: "shared", State: forge.ChangeCheckPassed},
		{Name: "shared", State: forge.ChangeCheckFailed},
	}, checksFromRollupContexts(contexts))
}

func checkSuite(
	event string,
	workflow string,
) struct {
	WorkflowRun struct {
		Event    string
		Workflow struct {
			Name string
		}
	}
} {
	var checkSuite struct {
		WorkflowRun struct {
			Event    string
			Workflow struct {
				Name string
			}
		}
	}
	checkSuite.WorkflowRun.Event = event
	checkSuite.WorkflowRun.Workflow.Name = workflow
	return checkSuite
}

func dateTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	assert.NoError(t, err)
	return parsed
}

func dateTimePtr(t *testing.T, value string) *time.Time {
	t.Helper()

	dt := dateTime(t, value)
	return &dt
}
