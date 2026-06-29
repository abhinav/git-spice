package forgejo

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// MergeCommandEnvironment returns Forgejo-specific variables for merge hooks.
func (r *Repository) MergeCommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_FORGEJO_PR_NUMBER": strconv.FormatInt(mustPR(id).Number, 10),
	}, nil
}
