package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.abhg.dev/gs/internal/gateway/github"
)

func TestReviewerNames(t *testing.T) {
	users, teams := reviewerNames([]string{
		" alice ",
		"acme/platform",
		"",
		"bob",
		"alice",
		" acme/platform ",
	})

	assert.Equal(t, []string{"alice", "bob", "alice"}, users)
	assert.Equal(t, []github.TeamName{
		{Organization: "acme", Slug: "platform"},
		{Organization: "acme", Slug: "platform"},
	}, teams)
}
