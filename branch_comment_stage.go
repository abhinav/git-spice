package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/spice/state"
	"go.abhg.dev/gs/internal/text"
	"go.abhg.dev/gs/internal/xec"
)

type branchCommentStageCmd struct {
	FileAndLine string `arg:"" optional:"" help:"File and line in the form file.go:42."`
	Message     string `short:"m" placeholder:"MSG" help:"Comment body. Opens editor if not provided."`
	Respond     string `placeholder:"THREAD_ID" help:"Thread ID to reply to instead of starting a new thread."`
	Branch      string `short:"b" placeholder:"BRANCH" predictor:"trackedBranches" help:"Branch to stage comment for. Defaults to current branch."`
}

func (*branchCommentStageCmd) Help() string {
	return text.Dedent(`
		Stages an inline comment for later batch submission.
		Provide the file and line number as file.go:42.

		If no message is given with -m, an editor is opened.

		Use --respond to reply to an existing thread
		instead of starting a new one.

		Staged comments are submitted together with
		'gs branch comment submit-staged'.
	`)
}

func (cmd *branchCommentStageCmd) Run(
	ctx context.Context,
	log *silog.Logger,
	wt *git.Worktree,
	store *state.Store,
	repo *git.Repository,
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

	staged, err := store.LoadStagedComments(ctx, branch)
	if err != nil {
		return fmt.Errorf("load staged comments: %w", err)
	}
	if staged == nil {
		staged = &state.StagedComments{NextID: 1}
	}

	comment := state.StagedComment{
		ID:       staged.NextID,
		File:     file,
		Line:     line,
		Body:     body,
		ThreadID: cmd.Respond,
	}
	staged.Comments = append(staged.Comments, comment)
	staged.NextID++

	if err := store.SaveStagedComments(
		ctx, branch, staged,
	); err != nil {
		return fmt.Errorf("save staged comments: %w", err)
	}

	if cmd.Respond != "" {
		log.Infof(
			"Staged reply sc-%d to thread %s.",
			comment.ID, cmd.Respond,
		)
	} else {
		log.Infof(
			"Staged comment sc-%d on %s:%d.",
			comment.ID, file, line,
		)
	}
	return nil
}

// parseFileAndLine parses a "file.go:42" argument
// into file and line components.
func parseFileAndLine(s string) (string, int, error) {
	file, start, _, err := parseFileAndRange(s)
	return file, start, err
}

// parseFileAndRange parses "file.go:42" or "file.go:42-50".
// In single-line form, end is zero; in range form, end > start.
func parseFileAndRange(s string) (file string, start, end int, err error) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", 0, 0, fmt.Errorf(
			"expected file:line or file:start-end, got %q", s,
		)
	}
	file = s[:idx]
	lineSpec := s[idx+1:]

	if before, after, ok := strings.Cut(lineSpec, "-"); ok {
		start, err = strconv.Atoi(before)
		if err != nil {
			return "", 0, 0, fmt.Errorf(
				"invalid range start in %q: %w", s, err,
			)
		}
		end, err = strconv.Atoi(after)
		if err != nil {
			return "", 0, 0, fmt.Errorf(
				"invalid range end in %q: %w", s, err,
			)
		}
		if start <= 0 || end <= 0 {
			return "", 0, 0, fmt.Errorf(
				"line numbers must be positive in %q", s,
			)
		}
		if end <= start {
			return "", 0, 0, fmt.Errorf(
				"range end must be greater than start in %q", s,
			)
		}
		return file, start, end, nil
	}

	start, err = strconv.Atoi(lineSpec)
	if err != nil {
		return "", 0, 0, fmt.Errorf(
			"invalid line number in %q: %w", s, err,
		)
	}
	if start <= 0 {
		return "", 0, 0, fmt.Errorf(
			"line number must be positive, got %d", start,
		)
	}
	return file, start, 0, nil
}

// editCommentBody opens an editor for the user
// to write a comment body.
// initial is pre-filled text (may be empty).
func editCommentBody(
	ctx context.Context,
	repo *git.Repository,
	initial string,
) (string, error) {
	tmpFile := filepath.Join(
		os.TempDir(), "GS_COMMENT_EDITMSG",
	)
	if err := os.WriteFile(
		tmpFile, []byte(initial), 0o644,
	); err != nil {
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
