package gitea

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// CommandEnvironment returns Gitea-specific variables for command hooks.
func (r *Repository) CommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_GITEA_PR_NUMBER": strconv.FormatInt(mustPR(id).Number, 10),
	}, nil
}
