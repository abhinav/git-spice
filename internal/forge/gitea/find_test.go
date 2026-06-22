package gitea

import (
	"testing"

	"github.com/stretchr/testify/assert"
	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func TestMatchesBranch_forkRepository(t *testing.T) {
	pr := &giteagw.PullRequest{
		Head: &giteagw.PRBranch{
			Label: "feature",
			Ref:   "feature",
			Repo: &giteagw.Repository{
				FullName: "test-reviewer/test-fork-repo",
			},
		},
	}

	assert.True(t, matchesBranch(
		pr,
		"feature",
		&RepositoryID{
			url:   "https://gitea.example.com/test-reviewer/test-fork-repo",
			owner: "test-reviewer",
			name:  "test-fork-repo",
		},
		"test-owner",
		"test-repo",
	))
	assert.False(t, matchesBranch(
		pr,
		"feature",
		nil,
		"test-owner",
		"test-repo",
	))
}
