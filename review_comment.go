package main

import (
	"context"

	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/text"
)

type reviewCommentCmd struct {
	Anchor  review.Anchor `arg:"" help:"Comment anchor: file.go, file.go:42, or file.go:42-50."`
	Message string        `short:"m" placeholder:"MSG" help:"Comment body. Opens editor if not provided."`
	Draft   bool          `negatable:"" default:"true" help:"Save the comment as a local draft instead of posting it."`
	Branch  string        `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to comment on. Defaults to the current branch."`
}

func (*reviewCommentCmd) Help() string {
	return text.Dedent(`
		Adds a review comment to the change request
		for the current branch.
		The anchor controls the comment scope:

		  file.go:42       anchored to that line
		  file.go:42-50    anchored to that line range
		  file.go          anchored to the file

		Comments are saved as local drafts by default.
		Use --no-draft to post immediately.

		If no message is given with -m, an editor is opened.
	`)
}

func (cmd *reviewCommentCmd) Run(
	ctx context.Context,
	handler ReviewHandler,
	drafts ReviewDraftHandler,
) error {
	req := &review.CommentRequest{
		Branch:  cmd.Branch,
		Anchor:  cmd.Anchor,
		Message: cmd.Message,
	}
	if cmd.Draft {
		return drafts.SaveCommentDraft(ctx, req)
	}
	return handler.PostComment(ctx, req)
}
