package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/reviewdiff"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCommentAddCmd struct {
	FileAndLine string `arg:"" optional:"" help:"File and line in the form file.go:42."`
	Message     string `short:"m" placeholder:"MSG" help:"Comment body. Opens editor if not provided."`
	Respond     string `placeholder:"THREAD_ID" help:"Thread ID to reply to instead of starting a new thread."`
	Branch      string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to add comment for. Defaults to current branch."`
}

func (*branchCommentAddCmd) Help() string {
	return text.Dedent(`
		Posts an inline comment immediately
		on the change request for the current branch.
		Provide the file and line number as file.go:42.

		If no message is given with -m, an editor is opened.

		Use --respond to reply to an existing thread
		instead of starting a new one.
	`)
}

func (cmd *branchCommentAddCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	repo *git.Repository,
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

	var file string
	var line int
	if cmd.Respond == "" {
		if cmd.FileAndLine == "" {
			return errors.New(
				"file:line argument is required " +
					"unless --respond is used",
			)
		}
		var err error
		file, line, err = parseFileAndLine(cmd.FileAndLine)
		if err != nil {
			return err
		}
	}

	body := cmd.Message
	if body == "" {
		var err error
		body, err = editCommentBody(
			ctx, repo, "" /* initial */)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment body, aborting")
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

	req := forge.SubmitReviewCommentRequest{
		Body: body,
	}

	if cmd.Respond != "" {
		threadIDs, err := loadReviewThreadIDs(
			ctx, reviewRepo, b.Change.ChangeID(),
		)
		if err != nil {
			return err
		}
		req.ReplyTo, err = reviewThreadID(threadIDs, cmd.Respond)
		if err != nil {
			return err
		}
	} else {
		// New comments use the selected branch's postimage coordinates. Check
		// that the requested line belongs to its review diff before submission.
		diff, err := wt.DiffBranchBytes(ctx, b.Base, branch)
		if err != nil {
			return fmt.Errorf("get diff: %w", err)
		}

		patch, err := reviewdiff.Parse(diff)
		if err != nil {
			return fmt.Errorf("parse diff: %w", err)
		}
		if !patch.ContainsLine(file, line) {
			return fmt.Errorf(
				"review diff does not contain %s:%d",
				file,
				line,
			)
		}

		req.Path = file
		req.Range = forge.ReviewThreadLine(line)
		req.Side = forge.ReviewThreadSideRight
	}

	result, err := reviewRepo.SubmitReview(
		ctx,
		b.Change.ChangeID(),
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{req},
		},
	)
	if err != nil {
		return fmt.Errorf("post review comment: %w", err)
	}
	if len(result.Comments) != 1 {
		return fmt.Errorf(
			"post review comment: forge returned %d comment results",
			len(result.Comments),
		)
	}

	log.Infof(
		"Posted comment %s on %s.",
		result.Comments[0].ThreadID.String(),
		b.Change.ChangeID(),
	)
	return nil
}
