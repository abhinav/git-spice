package shamhub

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChangeChecksState always reports ChecksPassed.
// ShamHub does not simulate CI/checks.
func (r *forgeRepository) ChangeChecksState(
	_ context.Context, _ forge.ChangeID,
) (forge.ChecksState, error) {
	return forge.ChecksPassed, nil
}
