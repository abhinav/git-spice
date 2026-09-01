package main

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type reviewReplyCmd struct {
	ThreadID string `arg:"" help:"Thread ID to reply to."`
	Message  string `short:"m" placeholder:"MSG" help:"Reply body. Opens editor if not provided."`
	Draft    bool   `negatable:"" default:"true" help:"Save the reply as a local draft instead of posting it."`
	Branch   string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch containing the thread. Defaults to the current branch."`
}

func (*reviewReplyCmd) Help() string {
	return text.Dedent(`
		Replies to a review thread on the change request
		for the current branch.

		Replies are saved as local drafts by default.
		Use --no-draft to post immediately.

		If no message is given with -m, an editor is opened.
	`)
}

func (cmd *reviewReplyCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	store *state.Store,
	repo *git.Repository,
	forgeRepo forge.Repository,
) error {
	branch, err := reviewBranch(ctx, wt, cmd.Branch)
	if err != nil {
		return err
	}
	body, err := reviewCommentBody(ctx, repo, cmd.Message, "")
	if err != nil {
		return err
	}

	if cmd.Draft {
		return saveReviewDraft(ctx, log, store, branch, state.StagedComment{
			Body:     body,
			ThreadID: cmd.ThreadID,
		})
	}

	b, reviewRepo, err := reviewRepositoryForBranch(
		ctx, svc, forgeRepo, branch,
	)
	if err != nil {
		return err
	}
	threadIDs, err := loadReviewThreadIDs(
		ctx,
		reviewRepo,
		b.Change.ChangeID(),
	)
	if err != nil {
		return err
	}
	threadID, err := reviewThreadID(threadIDs, cmd.ThreadID)
	if err != nil {
		return err
	}

	return postReviewComment(
		ctx,
		log,
		reviewRepo,
		b.Change.ChangeID(),
		forge.SubmitReviewCommentRequest{
			Body:    body,
			ReplyTo: threadID,
		},
	)
}
