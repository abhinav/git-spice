package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/gateway/github"
)

func TestGithubMergeOperationStatus_failed(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "WithMessage",
			message: "Required checks have not passed",
			want:    "GitHub asynchronous merge failed: Required checks have not passed",
		},
		{
			name:    "WithoutMessage",
			message: " ",
			want:    "GitHub asynchronous merge failed: GitHub did not provide a failure reason",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := githubMergeOperationStatus(&github.AsyncMergeResult{
				Status:  github.AsyncMergeStatusFailed,
				Message: tt.message,
			})
			assert.EqualError(t, err, tt.want)
		})
	}
}
