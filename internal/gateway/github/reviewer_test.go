package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGateway_RequestReviews(t *testing.T) {
	gateway := newResponseGateway(t, `{
		"data": {"requestReviews": {}}
	}`)
	require.NoError(t, gateway.RequestReviews(t.Context(), &RequestReviewsInput{PullRequestID: "PR_1"}))
}
