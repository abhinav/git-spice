package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateway_UpdatePullRequest(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"updatePullRequest": {}}
	}`)
	require.NoError(t, gateway.UpdatePullRequest(t.Context(), &UpdatePullRequestInput{PullRequestID: "PR_1"}))
}

func TestGateway_ClosePullRequest(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"updatePullRequest": {}}
	}`)
	require.NoError(t, gateway.ClosePullRequest(t.Context(), "PR_1"))
}

func TestGateway_ConvertPullRequestToDraft(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"convertPullRequestToDraft": {}}
	}`)
	require.NoError(t, gateway.ConvertPullRequestToDraft(t.Context(), "PR_1"))
}

func TestGateway_MarkPullRequestReadyForReview(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"markPullRequestReadyForReview": {}}
	}`)
	require.NoError(t, gateway.MarkPullRequestReadyForReview(t.Context(), "PR_1"))
}
