package forgetest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigSanitizers_GitHubRoleCollisions(t *testing.T) {
	cfg := ForgeConfig{
		Owner:     "abhinav",
		Repo:      "test-repo",
		ForkOwner: "abhinav-robot",
		ForkRepo:  "test-repo-fork",
		Reviewer:  "abhinav-robot",
		Assignee:  "abhinav",
	}

	got := applyTestSanitizers(
		`owner=abhinav fork=abhinav-robot reviewer=abhinav-robot assignee=abhinav`,
		ConfigSanitizers(cfg, CanonicalGitHubConfig()),
	)

	assert.Equal(t,
		`owner=test-owner fork=test-owner-robot reviewer=test-owner-robot assignee=test-owner`,
		got)
}

func applyTestSanitizers(s string, sanitizers []Sanitizer) string {
	for _, sanitizer := range sanitizers {
		s = strings.ReplaceAll(s, sanitizer.Replace, sanitizer.With)
	}
	return s
}
