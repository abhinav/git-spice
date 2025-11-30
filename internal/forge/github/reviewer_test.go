package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseReviewer(t *testing.T) {
	tests := []struct {
		name         string
		reviewer     string
		wantType     reviewerType
		wantReviewer string
	}{
		{
			name:         "User",
			reviewer:     "alice",
			wantType:     reviewerTypeUser,
			wantReviewer: "alice",
		},
		{
			name:         "Team",
			reviewer:     "org/team",
			wantType:     reviewerTypeTeam,
			wantReviewer: "org/team",
		},
		{
			name:         "TeamWithMultipleSlashes",
			reviewer:     "org/team/subteam",
			wantType:     reviewerTypeTeam,
			wantReviewer: "org/team/subteam",
		},
		{
			name:         "UserWithHyphen",
			reviewer:     "alice-bob",
			wantType:     reviewerTypeUser,
			wantReviewer: "alice-bob",
		},
		{
			name:         "UserWithUnderscore",
			reviewer:     "alice_bob",
			wantType:     reviewerTypeUser,
			wantReviewer: "alice_bob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotReviewer := parseReviewer(tt.reviewer)
			assert.Equal(t, tt.wantType, gotType)
			assert.Equal(t, tt.wantReviewer, gotReviewer)
		})
	}
}
