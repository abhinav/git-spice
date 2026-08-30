package main

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/reviewdiff"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
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
		return fmt.Errorf("load draft comments: %w", err)
	}
	if staged == nil {
		staged = &state.StagedComments{}
	}

	if len(staged.Comments) == 0 {
		log.Infof("No draft comments to publish.")
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

	reviewRepo, ok := forgeRepo.(forge.ReviewRepository)
	if !ok {
		return errors.New(
			"forge does not support review comments",
		)
	}

	// Draft roots use the selected branch's postimage coordinates. Parse the
	// review diff once so every root can be checked before anything is sent.
	diff, err := wt.OpenBranchDiff(ctx, b.Base, branch)
	if err != nil {
		return fmt.Errorf("open diff: %w", err)
	}
	patch, err := reviewdiff.Parse(diff)
	err = errors.Join(err, diff.Close())
	if err != nil {
		return fmt.Errorf("parse diff: %w", err)
	}

	var threadIDs map[string]forge.ReviewThreadID
	for _, comment := range staged.Comments {
		if comment.ThreadID == "" {
			continue
		}
		threadIDs, err = loadReviewThreadIDs(
			ctx, reviewRepo, b.Change.ChangeID(),
		)
		if err != nil {
			return err
		}
		break
	}

	var comments []forge.SubmitReviewCommentRequest
	for _, sc := range staged.Comments {
		if sc.ThreadID != "" {
			threadID, err := reviewThreadID(threadIDs, sc.ThreadID)
			if err != nil {
				return fmt.Errorf("draft %d: %w", sc.ID, err)
			}
			comments = append(comments,
				forge.SubmitReviewCommentRequest{
					Body:    sc.Body,
					ReplyTo: threadID,
				},
			)
			continue
		}

		if !patch.ContainsLine(sc.File, sc.Line) {
			return fmt.Errorf(
				"draft %d: review diff does not contain %s:%d",
				sc.ID,
				sc.File,
				sc.Line,
			)
		}
		comments = append(comments,
			forge.SubmitReviewCommentRequest{
				Path:  sc.File,
				Range: forge.ReviewThreadLine(sc.Line),
				Body:  sc.Body,
				Side:  forge.ReviewThreadSideRight,
			},
		)
	}

	disposition := forge.ReviewDispositionNone
	if cmd.Approve {
		disposition = forge.ReviewDispositionApprove
	} else if cmd.RequestChanges {
		disposition = forge.ReviewDispositionRequestChanges
	}

	if _, err := reviewRepo.SubmitReview(
		ctx,
		b.Change.ChangeID(),
		forge.SubmitReviewRequest{
			Body:        cmd.Body,
			Disposition: disposition,
			Comments:    comments,
		},
	); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}

	if err := store.ClearStagedComments(
		ctx, branch,
	); err != nil {
		return fmt.Errorf("clear draft comments: %w", err)
	}

	log.Infof(
		"Published %d comment(s) as review on %s.",
		len(comments),
		b.Change.ChangeID(),
	)
	return nil
}
