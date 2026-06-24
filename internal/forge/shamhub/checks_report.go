package shamhub

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
)

// ChecksByChange reports per-change rolled-up and per-run check state
// for each of the given changes.
//
// TODO: real implementation lands on a follow-up branch.
// This stub returns one nil per id to satisfy the [forge.Repository]
// interface while the schema branch lands standalone.
func (r *forgeRepository) ChecksByChange(
	_ context.Context, ids []forge.ChangeID,
) ([]*forge.ChecksReport, error) {
	return make([]*forge.ChecksReport, len(ids)), nil
}
