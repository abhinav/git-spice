package github

import (
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/forge"
)

func TestForgeReviewDecision(t *testing.T) {
	tests := []struct {
		name             string
		d                githubv4.PullRequestReviewDecision
		hasHumanReviewer bool
		want             forge.ChangeReviewDecision
	}{
		{
			name: "Approved",
			d:    githubv4.PullRequestReviewDecisionApproved,
			want: forge.ChangeReviewApproved,
		},
		{
			name: "ChangesRequested",
			d:    githubv4.PullRequestReviewDecisionChangesRequested,
			want: forge.ChangeReviewChangesRequested,
		},
		{
			name:             "ReviewRequired",
			d:                githubv4.PullRequestReviewDecisionReviewRequired,
			hasHumanReviewer: true,
			want:             forge.ChangeReviewRequired,
		},
		{
			name:             "ReviewRequiredBotOnly",
			d:                githubv4.PullRequestReviewDecisionReviewRequired,
			hasHumanReviewer: false,
			want:             forge.ChangeReviewNoReview,
		},
		{
			name: "NoReviewNoReviewers",
			want: forge.ChangeReviewNoReview,
		},
		{
			name:             "HumanReviewerPending",
			hasHumanReviewer: true,
			want:             forge.ChangeReviewRequired,
		},
		{
			name:             "BotOnlyReviewerIgnored",
			hasHumanReviewer: false,
			want:             forge.ChangeReviewNoReview,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forgeReviewDecision(tt.d, tt.hasHumanReviewer)
			assert.Equal(t, tt.want, got)
		})
	}
}
