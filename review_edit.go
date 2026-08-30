package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/text"
)

type reviewEditCmd struct {
	ID      int    `arg:"" help:"Draft comment ID to edit."`
	Message string `short:"m" placeholder:"MSG" help:"New comment body. Opens editor if not provided."`
	Branch  string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch containing the draft. Defaults to the current branch."`
}

func (*reviewEditCmd) Help() string {
	return text.Dedent(`
		Edits a local draft comment.

		Use 'gs review list --draft-only'
		to find the branch-local draft ID.

		If no message is given with -m, an editor is opened
		with the current comment body pre-filled.
	`)
}

func (cmd *reviewEditCmd) Run(
	ctx context.Context,
	handler ReviewDraftHandler,
) error {
	return handler.ReplaceDraftBody(ctx, &review.ReplaceDraftBodyRequest{
		Branch:  cmd.Branch,
		ID:      review.DraftID(cmd.ID),
		Message: cmd.Message,
	})
}
