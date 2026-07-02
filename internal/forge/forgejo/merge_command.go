package forgejo

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// CommandEnvironment returns Forgejo-specific variables for command hooks.
func (r *Repository) CommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_FORGEJO_PR_NUMBER": strconv.FormatInt(mustPR(id).Number, 10),
	}, nil
}
