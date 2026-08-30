package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
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
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	repo *git.Repository,
) error {
	branch, err := reviewBranch(ctx, wt, cmd.Branch)
	if err != nil {
		return err
	}

	staged, err := store.LoadStagedComments(ctx, branch)
	if err != nil {
		return fmt.Errorf("load draft comments: %w", err)
	}
	if staged == nil {
		staged = &state.StagedComments{}
	}

	idx := -1
	for i, comment := range staged.Comments {
		if comment.ID == cmd.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("draft comment %d not found", cmd.ID)
	}

	body, err := reviewCommentBody(
		ctx,
		repo,
		cmd.Message,
		staged.Comments[idx].Body,
	)
	if err != nil {
		return err
	}
	staged.Comments[idx].Body = body
	if err := store.SaveStagedComments(ctx, branch, staged); err != nil {
		return fmt.Errorf("save draft comments: %w", err)
	}

	log.Infof("Updated draft comment %d.", cmd.ID)
	return nil
}
