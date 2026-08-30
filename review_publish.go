package main

import (
	"context"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/handler/review"
	"go.abhg.dev/gs/internal/text"
)

type reviewPublishCmd struct {
	Body           string `placeholder:"BODY" help:"Overall review body."`
	Approve        bool   `help:"Mark the review as approved."`
	RequestChanges bool   `name:"request-changes" help:"Mark the review as requesting changes."`
	Branch         string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch whose draft comments to publish. Defaults to the current branch."`
}

func (*reviewPublishCmd) Help() string {
	return text.Dedent(`
		Publishes all draft comments for the current branch
		as a single review on the change request.

		Use --approve or --request-changes
		to set the review event type.
		Defaults to a comment-only review.

		Use --body to add an overall review body.
	`)
}

func (cmd *reviewPublishCmd) Run(
	ctx context.Context,
	handler ReviewHandler,
) error {
	disposition := forge.ReviewDispositionNone
	if cmd.Approve {
		disposition = forge.ReviewDispositionApprove
	} else if cmd.RequestChanges {
		disposition = forge.ReviewDispositionRequestChanges
	}

	return handler.PublishDrafts(ctx, &review.PublishDraftsRequest{
		Branch:      cmd.Branch,
		Body:        cmd.Body,
		Disposition: disposition,
	})
}
