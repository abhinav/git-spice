package bitbucket

import (
	"context"
	"errors"

	"go.abhg.dev/gs/internal/forge"
	gw "go.abhg.dev/gs/internal/gateway/bitbucket"
)

// EditChange edits an existing pull request.
func (r *Repository) EditChange(
	ctx context.Context,
	id forge.ChangeID,
	opts forge.EditChangeOptions,
) error {
	if len(opts.AddLabels) > 0 {
		r.log.Warn(r.gw.Product() +
			" does not support PR labels; ignoring --label flags")
	}
	if len(opts.AddAssignees) > 0 {
		r.log.Warn(r.gw.Product() +
			" does not support PR assignees; ignoring --assign flags")
	}

	num := mustPR(id).Number

	if opts.Base != "" || len(opts.AddReviewers) > 0 {
		err := r.gw.UpdateChange(ctx, num, gw.ChangeUpdate{
			Base:         opts.Base,
			AddReviewers: opts.AddReviewers,
		})
		if err != nil {
			return err
		}
	}

	if opts.Draft != nil {
		err := r.gw.SetChangeDraft(ctx, num, *opts.Draft)
		switch {
		case errors.Is(err, gw.ErrUnsupported):
			r.log.Warn(r.gw.Product() +
				" does not support toggling PR draft status" +
				" after creation; ignoring --draft/--ready")
		case err != nil:
			return err
		}
	}

	return nil
}
