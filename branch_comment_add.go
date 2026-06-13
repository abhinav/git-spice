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
	Anchor  string `arg:"" optional:"" help:"What to anchor the comment to: file.go:42 for a line, file.go for a file, or empty for the PR."`
	Message string `short:"m" placeholder:"MSG" help:"Comment body. Opens editor if not provided."`
	PR      bool   `name:"pr" help:"Post a PR-level comment with no file or line anchor."`
	Respond string `placeholder:"THREAD_ID" help:"Thread ID to reply to instead of starting a new thread."`
	Branch  string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to add comment for. Defaults to current branch."`
}

func (*branchCommentAddCmd) Help() string {
	return text.Dedent(`
		Posts a comment immediately on the change request for the
		current branch. The anchor argument controls the scope:

		  file.go:42       line-scope: anchored to that line
		  file.go:42-50    line-scope multi-line range
		  file.go          file-scope: anchored to the file
		  (empty) + --pr   pr-scope: not anchored to any file

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

	var (
		scope    forge.CommentScope
		file     string
		line     int
		rangeEnd int
	)
	if cmd.Respond == "" {
		var err error
		scope, file, line, rangeEnd, err = parseAnchor(
			cmd.Anchor, cmd.PR,
		)
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
		Scope:    scope,
		Body:     body,
		ThreadID: cmd.Respond,
	}

	switch {
	case cmd.Respond != "":
		// Reply: inherits the thread's existing anchor.
	case scope == forge.CommentScopePR:
		// No anchor to resolve.
	case scope == forge.CommentScopeFile:
		// File-scope: no line-level mapping needed; the path is
		// already what we want to post against.
		req.Path = file
	default:
		// Line scope: map file:line to diff coordinates.
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

		if rangeEnd > 0 {
			_, endDiffLine, _, err := mapper.Map(file, rangeEnd)
			if err != nil {
				return fmt.Errorf(
					"map %s:%d to diff: %w",
					file, rangeEnd, err,
				)
			}
			req.Range = &forge.CommentRange{
				Start: diffLine,
				End:   endDiffLine,
			}
		}
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

// parseAnchor decodes the optional positional argument to
// 'gs branch comment add', accounting for the --pr flag, into
// the comment's scope, path, line, and range-end.
//
// Forms:
//   - empty + pr=true    → CommentScopePR, no path/line
//   - "path"             → CommentScopeFile (path with no ':')
//   - "path:42"          → CommentScopeLine, line=42
//   - "path:42-50"       → CommentScopeLine, line=42, rangeEnd=50
func parseAnchor(
	arg string, pr bool,
) (scope forge.CommentScope, file string, line, rangeEnd int, err error) {
	if pr {
		if arg != "" {
			return 0, "", 0, 0, fmt.Errorf(
				"--pr takes no anchor argument, got %q", arg,
			)
		}
		return forge.CommentScopePR, "", 0, 0, nil
	}
	if arg == "" {
		return 0, "", 0, 0, errors.New(
			"anchor argument is required " +
				"(use --pr for a PR-level comment, " +
				"or --respond to reply to a thread)",
		)
	}
	if !strings.Contains(arg, ":") {
		return forge.CommentScopeFile, arg, 0, 0, nil
	}
	file, line, rangeEnd, err = parseFileAndRange(arg)
	if err != nil {
		return 0, "", 0, 0, err
	}
	return forge.CommentScopeLine, file, line, rangeEnd, nil
}
