package main

import (
	"context"
	"fmt"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/handler/review"
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
	wt *git.Worktree,
	handler ReviewHandler,
	drafts ReviewDraftHandler,
) error {
	if cmd.Branch == "" {
		branch, err := wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
		cmd.Branch = branch
	}

	req := &review.ReplyRequest{
		Branch:   cmd.Branch,
		ThreadID: cmd.ThreadID,
		Message:  cmd.Message,
	}
	if cmd.Draft {
		return drafts.SaveReplyDraft(ctx, req)
	}
	return handler.PostReply(ctx, req)
}
