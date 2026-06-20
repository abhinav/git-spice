package shamhub

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeMergeability reports whether the change can be merged.
//
// This compatibility implementation returns unknown until the ShamHub-specific
// mergeability simulation is implemented.
func (r *forgeRepository) ChangeMergeability(
	context.Context,
	forge.ChangeID,
) (forge.ChangeMergeability, error) {
	return forge.ChangeMergeability{}, nil
}
