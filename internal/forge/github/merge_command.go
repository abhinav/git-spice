package github

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// MergeCommandEnvironment returns GitHub-specific variables for merge hooks.
func (r *Repository) MergeCommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_GITHUB_PR_NUMBER": strconv.Itoa(mustPR(id).Number),
	}, nil
}
