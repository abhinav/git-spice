package bitbucket

import (
	"context"
	"strconv"

	"go.abhg.dev/gs/internal/forge"
)

// MergeCommandEnvironment returns Bitbucket-specific variables for merge hooks.
func (r *Repository) MergeCommandEnvironment(
	_ context.Context,
	id forge.ChangeID,
) (map[string]string, error) {
	return map[string]string{
		"GIT_SPICE_BITBUCKET_PR_ID": strconv.FormatInt(mustPR(id).Number, 10),
	}, nil
}
