package bitbucket

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the pull request can be merged.
//
// This compatibility implementation returns unknown until the Bitbucket-specific
// mergeability translation is implemented.
func (r *Repository) ChangeMergeability(
	context.Context,
	forge.ChangeID,
) (forge.ChangeMergeability, error) {
	return forge.ChangeMergeability{}, nil
}
