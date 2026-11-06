package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/text"
)

type reviewDeleteCmd struct {
	IDs    []review.DraftID `arg:"" name:"draft" help:"Draft comment IDs to delete."`
	Branch string           `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch containing the drafts. Defaults to the current branch."`
}

func (*reviewDeleteCmd) Help() string {
	return text.Dedent(`
		Deletes one or more local draft comments.

		Use 'gs review list --draft-only'
		to find the branch-local draft IDs.
	`)
}

func (cmd *reviewDeleteCmd) Run(
	ctx context.Context,
	wt *git.Worktree,
	handler ReviewDraftHandler,
) error {
	if cmd.Branch == "" {
		branch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = branch
	}

	return handler.DeleteDrafts(ctx, &review.DeleteDraftsRequest{
		Branch: cmd.Branch,
		IDs:    cmd.IDs,
	})
}
