package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/reviewdiff"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/xec"
)

type reviewCommentCmd struct {
	FileAndLine string `arg:"" help:"File and line in the form file.go:42."`
	Message     string `short:"m" placeholder:"MSG" help:"Comment body. Opens editor if not provided."`
	Draft       bool   `negatable:"" default:"true" help:"Save the comment as a local draft instead of posting it."`
	Branch      string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to comment on. Defaults to the current branch."`
}

func (*reviewCommentCmd) Help() string {
	return text.Dedent(`
		Adds a review comment to the change request
		for the current branch.
		Provide the file and line number as file.go:42.

		Comments are saved as local drafts by default.
		Use --no-draft to post immediately.

		If no message is given with -m, an editor is opened.
	`)
}

func (cmd *reviewCommentCmd) Run(
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

	file, line, err := parseFileAndLine(cmd.FileAndLine)
	if err != nil {
		return err
	}
	body, err := reviewCommentBody(ctx, repo, cmd.Message, "")
	if err != nil {
		return err
	}

	if cmd.Draft {
		return saveReviewDraft(ctx, log, store, branch, state.StagedComment{
			File: file,
			Line: line,
			Body: body,
		})
	}

	b, reviewRepo, err := reviewRepositoryForBranch(
		ctx, svc, forgeRepo, branch,
	)
	if err != nil {
		return err
	}
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

	return postReviewComment(
		ctx,
		log,
		reviewRepo,
		b.Change.ChangeID(),
		forge.SubmitReviewCommentRequest{
			Path:  file,
			Range: forge.ReviewThreadLine(line),
			Body:  body,
			Side:  forge.ReviewThreadSideRight,
		},
	)
}

func reviewBranch(
	ctx context.Context,
	wt *git.Worktree,
	branch string,
) (string, error) {
	if branch != "" {
		return branch, nil
	}
	branch, err := wt.CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return branch, nil
}

func reviewCommentBody(
	ctx context.Context,
	repo *git.Repository,
	message string,
	initial string,
) (string, error) {
	body := message
	if body == "" {
		var err error
		body, err = editReviewCommentBody(ctx, repo, initial)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(body) == "" {
		return "", errors.New("empty comment body, aborting")
	}
	return body, nil
}

func saveReviewDraft(
	ctx context.Context,
	log *silog.Logger,
	store *state.Store,
	branch string,
	comment state.StagedComment,
) error {
	staged, err := store.LoadStagedComments(ctx, branch)
	if err != nil {
		return fmt.Errorf("load draft comments: %w", err)
	}
	if staged == nil {
		staged = &state.StagedComments{NextID: 1}
	}

	comment.ID = staged.NextID
	staged.Comments = append(staged.Comments, comment)
	staged.NextID++
	if err := store.SaveStagedComments(ctx, branch, staged); err != nil {
		return fmt.Errorf("save draft comments: %w", err)
	}

	if comment.ThreadID != "" {
		log.Infof(
			"Drafted reply %d to thread %s.",
			comment.ID,
			comment.ThreadID,
		)
	} else {
		log.Infof(
			"Drafted comment %d on %s:%d.",
			comment.ID,
			comment.File,
			comment.Line,
		)
	}
	return nil
}

func postReviewComment(
	ctx context.Context,
	log *silog.Logger,
	reviewRepo forge.ReviewRepository,
	changeID forge.ChangeID,
	comment forge.SubmitReviewCommentRequest,
) error {
	result, err := reviewRepo.SubmitReview(
		ctx,
		changeID,
		forge.SubmitReviewRequest{
			Comments: []forge.SubmitReviewCommentRequest{comment},
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
		changeID,
	)
	return nil
}

// parseFileAndLine parses a file.go:42 argument into its path and line.
func parseFileAndLine(value string) (string, int, error) {
	idx := strings.LastIndex(value, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf(
			"expected file:line format, got %q", value,
		)
	}
	file := value[:idx]
	line, err := strconv.Atoi(value[idx+1:])
	if err != nil {
		return "", 0, fmt.Errorf(
			"invalid line number in %q: %w", value, err,
		)
	}
	if line <= 0 {
		return "", 0, fmt.Errorf(
			"line number must be positive, got %d", line,
		)
	}
	return file, line, nil
}

// editReviewCommentBody opens the configured editor with initial as its
// starting contents and returns the edited comment body.
func editReviewCommentBody(
	ctx context.Context,
	repo *git.Repository,
	initial string,
) (string, error) {
	tmpFile := filepath.Join(os.TempDir(), "GS_REVIEW_EDITMSG")
	if err := os.WriteFile(tmpFile, []byte(initial), 0o644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	editor := gitEditor(ctx, repo)
	cmd := xec.EditCommand(editor, tmpFile)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run editor: %w", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}
	return string(content), nil
}
