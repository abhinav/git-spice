package github

import (
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
)

func TestChecksFromRollupContexts_statusContexts(t *testing.T) {
	var contexts []statusCheckRollupContext
	contexts = append(contexts,
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context: "git-spice integration",
				State:   githubv4.StatusStateSuccess,
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context: "lint",
				State:   githubv4.StatusStatePending,
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context: "test",
				State:   githubv4.StatusStateFailure,
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
	successConclusion := githubv4.CheckConclusionStateSuccess
	failureConclusion := githubv4.CheckConclusionStateFailure

	var contexts []statusCheckRollupContext
	contexts = append(contexts,
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:   "build",
				Status: githubv4.CheckStatusStateInProgress,
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:       "unit",
				Status:     githubv4.CheckStatusStateCompleted,
				Conclusion: &successConclusion,
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:       "integration",
				Status:     githubv4.CheckStatusStateCompleted,
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
	successConclusion := githubv4.CheckConclusionStateSuccess
	failureConclusion := githubv4.CheckConclusionStateFailure

	var contexts []statusCheckRollupContext
	contexts = append(contexts,
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "unit",
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "unit",
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:05:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "integration",
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:10:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "integration",
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:01:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "test",
				CheckSuite:  checkSuite("push", "linux"),
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "test",
				CheckSuite:  checkSuite("pull_request", "linux"),
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &failureConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "test",
				CheckSuite:  checkSuite("push", "windows"),
				Status:      githubv4.CheckStatusStateCompleted,
				Conclusion:  &successConclusion,
				CompletedAt: dateTimePtr(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context:   "deploy",
				State:     githubv4.StatusStatePending,
				CreatedAt: dateTime(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context:   "deploy",
				State:     githubv4.StatusStateSuccess,
				CreatedAt: dateTime(t, "2026-06-19T10:05:00Z"),
			},
		},
		statusCheckRollupContext{
			StatusContext: statusContextRollupContext{
				Context:   "shared",
				State:     githubv4.StatusStateSuccess,
				CreatedAt: dateTime(t, "2026-06-19T10:00:00Z"),
			},
		},
		statusCheckRollupContext{
			CheckRun: checkRunRollupContext{
				Name:        "shared",
				Status:      githubv4.CheckStatusStateCompleted,
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
		Event    string `graphql:"event"`
		Workflow struct {
			Name string `graphql:"name"`
		} `graphql:"workflow"`
	} `graphql:"workflowRun"`
} {
	var checkSuite struct {
		WorkflowRun struct {
			Event    string `graphql:"event"`
			Workflow struct {
				Name string `graphql:"name"`
			} `graphql:"workflow"`
		} `graphql:"workflowRun"`
	}
	checkSuite.WorkflowRun.Event = event
	checkSuite.WorkflowRun.Workflow.Name = workflow
	return checkSuite
}

func dateTime(t *testing.T, value string) githubv4.DateTime {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	assert.NoError(t, err)
	return githubv4.DateTime{Time: parsed}
}

func dateTimePtr(t *testing.T, value string) *githubv4.DateTime {
	t.Helper()

	dt := dateTime(t, value)
	return &dt
}
