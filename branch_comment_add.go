package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.abhg.dev/gs/internal/diffmap"
	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
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

	inline, ok := forgeRepo.(forge.WithInlineComments)
	if !ok {
		return errors.New(
			"forge does not support inline comments",
		)
	}

	req := forge.InlineCommentRequest{
		Body:     body,
		ThreadID: cmd.Respond,
	}

	// Map file:line to diff coordinates
	// if this is a new comment (not a reply).
	if cmd.Respond == "" {
		diff, err := wt.DiffBranchBytes(ctx, b.Base, branch)
		if err != nil {
			return fmt.Errorf("get diff: %w", err)
		}

		mapper, err := diffmap.New(diff)
		if err != nil {
			return fmt.Errorf("parse diff: %w", err)
		}

		path, diffLine, side, err := mapper.Map(file, line)
		if err != nil {
			return fmt.Errorf(
				"map %s:%d to diff: %w",
				file, line, err,
			)
		}

		req.Path = path
		req.Line = diffLine
		req.Side = side
	}

	posted, err := inline.PostInlineComment(
		ctx, b.Change.ChangeID(), req,
	)
	if err != nil {
		return fmt.Errorf("post inline comment: %w", err)
	}

	log.Infof(
		"Posted comment %s on %s.",
		posted.ID, b.Change.ChangeID(),
	)
	return nil
}
