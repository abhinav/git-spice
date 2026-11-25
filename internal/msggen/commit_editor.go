package msggen

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.abhg.dev/gs/internal/git"
	"go.abhg.dev/gs/internal/scriptrun"
)

const (
	_fillCommandEnv    = "__GS_FILL_COMMAND"
	_originalEditorEnv = "__GS_FILL_ORIGINAL_EDITOR"
)

// CommitEditor configures Git to run a fill command on its prepared message.
type CommitEditor struct {
	Fill    bool
	Command string
}

// Enabled reports whether automatic filling was selected.
func (e *CommitEditor) Enabled() bool { return e.Fill }

// CommitEdit arranges for Git to invoke this executable with its
// prepared message and GIT_INDEX_FILE. Git appends the message path.
// An empty Command returns ErrNoCommand.
func (e *CommitEditor) CommitEdit(operation OperationKind) (git.CommitEdit, error) {
	if e.Command == "" {
		return git.CommitEdit{}, ErrNoCommand
	}
	exe, err := os.Executable()
	if err != nil {
		return git.CommitEdit{}, fmt.Errorf("resolve message editor executable: %w", err)
	}
	var editorEnv string
	if value, ok := os.LookupEnv("GIT_EDITOR"); ok {
		editorEnv = "GIT_EDITOR=" + value
	}

	// Git prints the editor invocation on failure. Keep the configured
	// command and editor out of that diagnostic.
	return git.EditCommitWith(git.CommitEditor{
		Args: []string{exe, "internal", "fill-message", "--no-prompt", operation.String()},
		Env:  []string{_fillCommandEnv + "=" + e.Command, _originalEditorEnv + "=" + editorEnv},
	}), nil
}

// EditRequest describes an edit of a message file prepared by Git.
type EditRequest struct {
	File string // required

	// Operation is the invoking git-spice command with its words hyphenated.
	Operation string // required
}

// RunCommitEditor edits Git's prepared message with the command captured by
// CommitEdit. It restores the user's editor environment in this child process.
func RunCommitEditor(ctx context.Context, req *EditRequest, runner ScriptRunner) error {
	command := os.Getenv(_fillCommandEnv)
	editorEnv := os.Getenv(_originalEditorEnv)
	_ = os.Unsetenv(_fillCommandEnv)
	_ = os.Unsetenv(_originalEditorEnv)

	// Let scripts resolve the user's editor normally. This helper runs in
	// its own process, so the override can be removed from its environment.
	var err error
	if value, ok := strings.CutPrefix(editorEnv, "GIT_EDITOR="); ok {
		err = os.Setenv("GIT_EDITOR", value)
	} else {
		err = os.Unsetenv("GIT_EDITOR")
	}
	if err != nil {
		return fmt.Errorf("restore editor environment: %w", err)
	}

	return runMessageCommand(ctx, runner, &scriptrun.RunRequest{
		Script: command,
		Args:   []string{req.File, req.Operation},
	})
}
