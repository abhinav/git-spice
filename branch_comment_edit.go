package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
)

type branchCommentEditCmd struct {
	ID      string `arg:"" help:"Comment ID to edit. Use 'sc-N' for staged comments or a forge comment ID."`
	Message string `short:"m" placeholder:"MSG" help:"New comment body. Opens editor if not provided."`
	Branch  string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch whose comments to edit. Defaults to current branch."`
}

func (*branchCommentEditCmd) Help() string {
	return text.Dedent(`
		Edits the body of a comment.

		For staged comments (sc-N prefix),
		the comment is updated in the local staging area.

		For forge comments, the comment is updated
		on the remote forge.

		If no message is given with -m, an editor is opened
		with the current comment body pre-filled.
	`)
}

func (cmd *branchCommentEditCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	svc *spice.Service,
	store *state.Store,
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

	// Handle staged comment edits.
	if scID, ok := parseStagedCommentID(cmd.ID); ok {
		return cmd.editStaged(
			ctx, log, store, repo, branch, scID,
		)
	}

	// Handle forge comment edits.
	return cmd.editForge(
		ctx, log, wt, svc, repo, forgeRepo, branch,
	)
}

func (cmd *branchCommentEditCmd) editStaged(
	ctx context.Context,
	log *silog.Logger,
	store *state.Store,
	repo *git.Repository,
	branch string,
	scID int,
) error {
	staged, err := store.LoadStagedComments(ctx, branch)
	if err != nil {
		return fmt.Errorf("load staged comments: %w", err)
	}
	if staged == nil {
		staged = &state.StagedComments{}
	}

	idx := -1
	for i, c := range staged.Comments {
		if c.ID == scID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf(
			"staged comment sc-%d not found", scID,
		)
	}

	body := cmd.Message
	if body == "" {
		var err error
		body, err = editCommentBody(
			ctx, repo, staged.Comments[idx].Body,
		)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment body, aborting")
	}

	staged.Comments[idx].Body = body
	if err := store.SaveStagedComments(
		ctx, branch, staged,
	); err != nil {
		return fmt.Errorf("save staged comments: %w", err)
	}

	log.Infof("Updated staged comment sc-%d.", scID)
	return nil
}

func (cmd *branchCommentEditCmd) editForge(
	ctx context.Context,
	log *silog.Logger,
	_ *git.Worktree,
	svc *spice.Service,
	repo *git.Repository,
	forgeRepo forge.Repository,
	branch string,
) error {
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
			"no change request for %s", branch,
		)
	}

	editor, ok := forgeRepo.(forge.WithCommentEdit)
	if !ok {
		return errors.New(
			"forge does not support comment editing",
		)
	}

	// Find the comment by ID to get its current body.
	inline, ok := forgeRepo.(forge.WithInlineComments)
	if !ok {
		return errors.New(
			"forge does not support inline comments",
		)
	}

	comments, err := inline.ListInlineComments(
		ctx, b.Change.ChangeID(),
	)
	if err != nil {
		return fmt.Errorf("list comments: %w", err)
	}

	var target *forge.InlineComment
	for _, c := range comments {
		if c.ID.String() == cmd.ID {
			target = c
			break
		}
	}
	if target == nil {
		return fmt.Errorf(
			"comment %s not found", cmd.ID,
		)
	}

	body := cmd.Message
	if body == "" {
		var err error
		body, err = editCommentBody(
			ctx, repo, target.Body,
		)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment body, aborting")
	}

	if err := editor.EditComment(
		ctx, target.ID, body,
	); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}

	log.Infof("Updated comment %s.", cmd.ID)
	return nil
}

// parseStagedCommentID parses "sc-N" into integer N.
// Returns (N, true) on success, (0, false) otherwise.
func parseStagedCommentID(s string) (int, bool) {
	after, found := strings.CutPrefix(s, "sc-")
	if !found {
		return 0, false
	}
	id, err := strconv.Atoi(after)
	if err != nil {
		return 0, false
	}
	return id, true
}
