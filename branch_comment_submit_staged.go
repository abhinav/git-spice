package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/diffmap"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCommentSubmitStagedCmd struct {
	Body           string `placeholder:"BODY" help:"Overall review body."`
	Approve        bool   `help:"Mark the review as approved."`
	RequestChanges bool   `name:"request-changes" help:"Mark the review as requesting changes."`
	Branch         string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to submit staged comments for. Defaults to current branch."`
}

func (*branchCommentSubmitStagedCmd) Help() string {
	return text.Dedent(`
		Submits all staged comments for the current branch
		as a single review on the change request.

		Use --approve or --request-changes
		to set the review event type.
		Defaults to a comment-only review.

		Use --body to add an overall review body.
	`)
}

func (cmd *branchCommentSubmitStagedCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	store *state.Store,
	forgeRepo forge.Repository,
) error {
	branch := cmd.Branch
	if branch == "" {
		var err error
		branch, err = wt.CurrentBranch(ctx)
		if err != nil {
			return fmt.Errorf("get current branch: %w", err)
		}
	}

	staged, err := store.LoadStagedComments(ctx, branch)
	if err != nil {
		return fmt.Errorf("load staged comments: %w", err)
	}
	if staged == nil {
		staged = &state.StagedComments{}
	}

	if len(staged.Comments) == 0 {
		log.Infof("No staged comments to submit.")
		return nil
	}

	b, err := svc.LookupBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, state.ErrNotExist) {
			return fmt.Errorf(
				"branch not tracked: %s", branch,
			)
		}
		return fmt.Errorf("get branch: %w", err)
	}

	if b.Change == nil {
		return fmt.Errorf(
			"no change request for %s; "+
				"submit the branch first with "+
				"'gs branch submit'",
			branch,
		)
	}

	inline, ok := forgeRepo.(forge.WithInlineComments)
	if !ok {
		return errors.New(
			"forge does not support inline comments",
		)
	}

	// Build diff map for coordinate translation.
	diff, err := wt.DiffBranchBytes(ctx, b.Base, branch)
	if err != nil {
		return fmt.Errorf("get diff: %w", err)
	}

	mapper, err := diffmap.New(diff)
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}

	// Map staged comments to forge requests.
	var comments []forge.InlineCommentRequest
	for _, sc := range staged.Comments {
		if sc.ThreadID != "" {
			// Thread reply: no coordinate mapping needed.
			comments = append(comments,
				forge.InlineCommentRequest{
					Body:     sc.Body,
					ThreadID: sc.ThreadID,
				},
			)
			continue
		}

		path, diffLine, side, err := mapper.Map(
			sc.File, sc.Line,
		)
		if err != nil {
			return fmt.Errorf(
				"sc-%d: map %s:%d to diff: %w",
				sc.ID, sc.File, sc.Line, err,
			)
		}

		comments = append(comments,
			forge.InlineCommentRequest{
				Path: path,
				Line: diffLine,
				Body: sc.Body,
				Side: side,
			},
		)
	}

	event := forge.ReviewComment
	if cmd.Approve {
		event = forge.ReviewApprove
	} else if cmd.RequestChanges {
		event = forge.ReviewRequestChanges
	}

	if err := inline.SubmitReview(
		ctx,
		b.Change.ChangeID(),
		forge.ReviewRequest{
			Body:     cmd.Body,
			Comments: comments,
			Event:    event,
		},
	); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}

	if err := store.ClearStagedComments(
		ctx, branch,
	); err != nil {
		return fmt.Errorf("clear staged comments: %w", err)
	}

	log.Infof(
		"Submitted %d comment(s) as review on %s.",
		len(comments),
		b.Change.ChangeID(),
	)
	return nil
}
