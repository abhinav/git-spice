package github

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// CommandEnvironment returns GitHub-specific variables for command hooks.
func (r *Repository) CommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_GITHUB_PR_NUMBER": strconv.Itoa(mustPR(id).Number),
	}, nil
}
