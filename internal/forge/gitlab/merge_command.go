package gitlab

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// MergeCommandEnvironment returns GitLab-specific variables for merge hooks.
func (r *Repository) MergeCommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_GITLAB_MR_IID": strconv.FormatInt(mustMR(id).Number, 10),
	}, nil
}
